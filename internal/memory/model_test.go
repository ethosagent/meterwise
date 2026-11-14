package memory_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/ethosagent/meterwise/internal/memory"
	"github.com/ethosagent/meterwise/internal/store"
)

const goodTrace = `{"task_summary":"fix parser","observations":["a","b"],"approach":"read the lexer","fix":"guard nil",
"signature":{"repo":null,"files":["lex/a.go"],"errors":["undefined"],"tools":["edit_file"]},"confidence":0.8}`

func TestParseTraceIsStrict(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		fail bool
	}{
		"valid":                 {goodTrace, false},
		"unknown field":         {strings.Replace(goodTrace, `"confidence"`, `"extra":1,"confidence"`, 1), true},
		"missing confidence":    {strings.Replace(goodTrace, `,"confidence":0.8`, ``, 1), true},
		"confidence over one":   {strings.Replace(goodTrace, `0.8`, `1.5`, 1), true},
		"empty fix":             {strings.Replace(goodTrace, `"guard nil"`, `"  "`, 1), true},
		"trailing text":         {goodTrace + ` and more`, true},
		"not json":              {`I cannot help`, true},
		"empty":                 {``, true},
		"second object follows": {goodTrace + goodTrace, true},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := memory.ParseTrace(tc.in)
			if (err != nil) != tc.fail {
				t.Fatalf("err = %v, want failure=%v", err, tc.fail)
			}
		})
	}
}

func TestParseTraceRedactsAndClamps(t *testing.T) {
	long := strings.Repeat("é", 400)
	in := `{"task_summary":"` + long + `","observations":["x","y","z","1","2","3","4"],"approach":"use sk-ant-abcdef123456 never","fix":"Authorization: Bearer abcdef123456",
"signature":{"repo":"r","files":[],"errors":[],"tools":[]},"confidence":0.5}`
	tr, err := memory.ParseTrace(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.TaskSummary) > 300 || !utf8.ValidString(tr.TaskSummary) || len(tr.Observations) != 5 {
		t.Fatalf("not clamped: %d bytes, %d observations", len(tr.TaskSummary), len(tr.Observations))
	}
	if strings.Contains(tr.Approach+tr.Fix, "abcdef123456") {
		t.Fatalf("not redacted: %q %q", tr.Approach, tr.Fix)
	}
}

func TestRedact(t *testing.T) {
	for in, leak := range map[string]string{
		"key sk-proj-abcdefgh12345678 end":     "abcdefgh12345678",
		"sk-ant-api03-xyzxyzxyz":               "xyzxyzxyz",
		"Authorization: Bearer eyJhbGciOi.abc": "eyJhbGciOi",
		"export OPENAI_API_KEY=abcd1234efgh":   "abcd1234efgh",
		"AKIAABCDEFGHIJKLMNOP":                 "AKIAABCDEFGHIJKLMNOP",
		"ghp_abcdefghijklmnopqrstuvwxyz0123":   "ghp_abcdefghij",
		"password: hunter2hunter2":             "hunter2hunter2",
	} {
		if got := memory.Redact(in); strings.Contains(got, leak) {
			t.Errorf("Redact(%q) = %q still holds %q", in, got, leak)
		}
	}
	if got := memory.Redact("fix the skill-tree in lex/a.go"); got != "fix the skill-tree in lex/a.go" {
		t.Errorf("Redact changed harmless text: %q", got)
	}
}

func TestRenderHintIsOneBoundedLine(t *testing.T) {
	h := memory.RenderHint("read the lexer.\n  first", "guard the nil token", []string{"a.go", "b.go", "c.go", "d.go"})
	if !strings.HasPrefix(h, memory.HintPrefix) || strings.ContainsAny(h, "\n\r") || strings.Contains(h, "d.go") || !strings.Contains(h, "a.go, b.go, c.go") {
		t.Fatalf("hint = %q", h)
	}
	huge := memory.RenderHint(strings.Repeat("é", 500), strings.Repeat("x", 500), nil)
	if len(huge) > memory.HintMax || !utf8.ValidString(huge) {
		t.Fatalf("hint is %d bytes (valid utf8 %v), want at most %d", len(huge), utf8.ValidString(huge), memory.HintMax)
	}
}

// model is a loopback twin of an OpenAI-compatible API.
func model(t *testing.T, status int, seen *atomic.Value) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if seen != nil {
			seen.Store(r.Header.Get("Authorization") + "|" + r.URL.Path + "|" + string(b))
		}
		if status != 200 {
			http.Error(w, "secret-upstream-body", status)
			return
		}
		switch r.URL.Path {
		case "/embeddings":
			v := make([]float32, store.EmbedDim)
			v[3] = 0.5
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"embedding": v}}, "usage": map[string]any{"prompt_tokens": 7}})
		case "/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": goodTrace}}}, "usage": map[string]any{"prompt_tokens": 90, "completion_tokens": 30}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEmbedderAndDistillerTalkToTheModel(t *testing.T) {
	var seen atomic.Value
	srv := model(t, 200, &seen)
	vec, u, err := memory.NewEmbedder(srv.URL, "key-123").Embed(t.Context(), "emb-model", "fix the parser")
	if err != nil || len(vec) != store.EmbedDim || vec[3] != 0.5 || u.Input != 7 {
		t.Fatalf("Embed = %d values, %+v, %v", len(vec), u, err)
	}
	got := seen.Load().(string)
	if !strings.HasPrefix(got, "Bearer key-123|/embeddings|") || !strings.Contains(got, `"model":"emb-model"`) || !strings.Contains(got, `"dimensions":1536`) {
		t.Fatalf("request = %s", got)
	}
	tr, u, err := memory.NewDistiller(srv.URL, "key-123").Distil(t.Context(), "m", memory.Summary{Task: "fix parser", Calls: 5, Tools: []string{"edit_file"}})
	if err != nil || tr.Fix != "guard nil" || u.Input != 90 || u.Output != 30 {
		t.Fatalf("Distil = %+v %+v %v", tr, u, err)
	}
	if got := seen.Load().(string); !strings.Contains(got, `"json_object"`) || !strings.Contains(got, "fix parser") {
		t.Fatalf("distil request = %s", got)
	}
}

func TestModelErrorsNeverCarryTheKeyOrTheBody(t *testing.T) {
	srv := model(t, 500, nil)
	_, _, err := memory.NewEmbedder(srv.URL, "key-123").Embed(t.Context(), "m", "text")
	if err == nil {
		t.Fatal("a 500 must fail")
	}
	if s := err.Error(); strings.Contains(s, "key-123") || strings.Contains(s, "secret-upstream-body") || !strings.Contains(s, "500") {
		t.Fatalf("error = %q", s)
	}
	if _, _, err := memory.NewEmbedder("http://127.0.0.1:1", "key-123").Embed(t.Context(), "m", "text"); err == nil || strings.Contains(err.Error(), "key-123") {
		t.Fatalf("unreachable endpoint error = %v", err)
	}
	if _, _, err := memory.NewEmbedder(srv.URL, "k").Embed(t.Context(), "m", "   "); err == nil {
		t.Fatal("nothing to embed must fail before any request")
	}
}

func TestEmbedderCutsLongTextOnACharacterBoundary(t *testing.T) {
	for name, text := range map[string]string{"ascii": strings.Repeat("a", 5000), "cjk": strings.Repeat("修", 2000)} {
		var seen atomic.Value
		srv := model(t, 200, &seen)
		if _, _, err := memory.NewEmbedder(srv.URL, "k").Embed(t.Context(), "m", text); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var req struct{ Input string }
		body := strings.SplitN(seen.Load().(string), "|", 3)[2]
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(req.Input) > 2000 || len(req.Input) < 1998 || !utf8.ValidString(req.Input) {
			t.Errorf("%s: input is %d bytes (valid utf-8 %v), want at most 2000 and whole characters", name, len(req.Input), utf8.ValidString(req.Input))
		}
	}
}
