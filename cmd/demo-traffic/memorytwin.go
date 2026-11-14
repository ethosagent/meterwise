package main

import (
	"encoding/json"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/ethosagent/meterwise/internal/store"
)

// modelTwin is the loopback twin of the OpenAI-compatible API memory calls with its own key (MEMORY_BASE_URL): an
// embeddings endpoint that hashes words into a unit vector, so two texts that share words are close and equal texts are
// identical, and a chat endpoint that answers every distil request with one canned, well-formed trace. It checks the
// bearer key it was started with. No model and no provider key is involved anywhere in a demo run.
type modelTwin struct {
	*httptest.Server
	key                       string
	embeds, distils, rejected atomic.Int64
}

func newModelTwin(key string) *modelTwin {
	t := &modelTwin{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /embeddings", t.embeddings)
	mux.HandleFunc("POST /chat/completions", t.chat)
	t.Server = httptest.NewServer(mux)
	return t
}

func (t *modelTwin) authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+t.key {
		t.rejected.Add(1)
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func (t *modelTwin) embeddings(w http.ResponseWriter, r *http.Request) {
	if !t.authorized(w, r) {
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var in struct {
		Input string `json:"input"`
	}
	if json.Unmarshal(raw, &in) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	t.embeds.Add(1)
	words := strings.FieldsFunc(strings.ToLower(in.Input), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data":  []any{map[string]any{"embedding": hashVector(words)}},
		"usage": map[string]any{"prompt_tokens": len(words), "total_tokens": len(words)},
	})
}

// hashVector puts each word on one of store.EmbedDim axes and scales the result to length 1.
func hashVector(words []string) []float32 {
	v := make([]float32, store.EmbedDim)
	for _, w := range words {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		v[h.Sum32()%store.EmbedDim]++
	}
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		v[0], sum = 1, 1
	}
	norm := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= norm
	}
	return v
}

// demoTrace is the one trace the twin's distiller returns: what a good distillation of the memory scenario looks like.
const demoTrace = `{"task_summary":"Fix the failing parser test","observations":["The test fails on an empty token stream"],` +
	`"approach":"Read lex.go to see how the token stream ends, then guard the empty case","fix":"Return early from Next() when the token stream is empty",` +
	`"signature":{"repo":null,"files":["parser/lex.go"],"errors":["undefined: lexer.Next"],"tools":["read_file","edit_file","run_tests"]},"confidence":0.85}`

func (t *modelTwin) chat(w http.ResponseWriter, r *http.Request) {
	if !t.authorized(w, r) {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	t.distils.Add(1)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": demoTrace}}},
		"usage":   map[string]any{"prompt_tokens": 120, "completion_tokens": 60},
	})
}
