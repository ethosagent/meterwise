package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

func lookup(kv map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
}

type result struct {
	OK    bool            `json:"ok"`
	Data  map[string]any  `json:"data"`
	Error *contract.Error `json:"error"`
}

func exec(t *testing.T, kv map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runCtx(context.Background(), args, lookup(kv), &out, &errb)
	return code, out.String(), errb.String()
}

func decode(t *testing.T, out string) result {
	t.Helper()
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("-json must print exactly one document, got %q", out)
	}
	var r result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("not JSON: %q: %v", out, err)
	}
	return r
}

func TestEverySubcommandAcceptsJSON(t *testing.T) {
	good := map[string]string{"METERWISE_DATABASE_URL": "postgres://u:p@127.0.0.1:5432/db"}
	tests := []struct {
		args     []string
		wantOK   bool
		wantCode contract.Code
		exit     int
	}{
		{[]string{"version", "-json"}, true, "", 0},
		{[]string{"config", "check", "-json"}, true, "", 0},
		{[]string{"migrate", "-json"}, false, contract.CodeNotAvailable, 1},
		{[]string{"bootstrap-admin", "-json"}, false, contract.CodeNotAvailable, 1},
		{[]string{"help", "-json"}, true, "", 0},
		{[]string{"nonsense"}, false, "", 2},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			code, out, _ := exec(t, good, tc.args...)
			if code != tc.exit {
				t.Fatalf("exit %d, want %d (out %q)", code, tc.exit, out)
			}
			if tc.args[0] == "nonsense" {
				return
			}
			r := decode(t, out)
			if r.OK != tc.wantOK {
				t.Fatalf("ok=%v, want %v", r.OK, tc.wantOK)
			}
			if !tc.wantOK && r.Error.Code != tc.wantCode {
				t.Fatalf("code %s, want %s", r.Error.Code, tc.wantCode)
			}
		})
	}
}

func TestConfigCheckJSONListsEveryProblemAtOnce(t *testing.T) {
	code, out, _ := exec(t, map[string]string{
		"METERWISE_LISTEN": "bad", "METERWISE_LOG_LEVEL": "loud", "METERWISE_ENV": "moon",
	}, "config", "check", "-json")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	r := decode(t, out)
	if r.Error.Code != contract.CodeConfigInvalid || len(r.Error.Details) != 4 {
		t.Fatalf("want config_invalid with 4 problems, got %+v", r.Error)
	}
}

func TestConfigCheckNeverPrintsThePassword(t *testing.T) {
	_, out, _ := exec(t, map[string]string{"METERWISE_DATABASE_URL": "postgres://u:hunter2@127.0.0.1:5432/db"}, "config", "check", "-json")
	if strings.Contains(out, "hunter2") {
		t.Fatalf("password leaked: %s", out)
	}
	_, out, _ = exec(t, map[string]string{"METERWISE_DATABASE_URL": "postgres://u:hunter2@127.0.0.1:5432/db"}, "config", "check")
	if strings.Contains(out, "hunter2") || !strings.Contains(out, "METERWISE_LISTEN=:8080") {
		t.Fatalf("text output wrong: %s", out)
	}
}

func TestTextModeAndUsageErrors(t *testing.T) {
	code, out, _ := exec(t, nil, "version")
	if code != 0 || !strings.Contains(out, "version=dev") {
		t.Fatalf("version text: %d %q", code, out)
	}
	code, out, _ = exec(t, nil)
	if code != 0 || !strings.Contains(out, "usage: meterwised") {
		t.Fatalf("help text: %d %q", code, out)
	}
	code, _, errb := exec(t, nil, "migrate")
	if code != 1 || !strings.Contains(errb, "not_available") {
		t.Fatalf("migrate text: %d %q", code, errb)
	}
	if code, _, _ = exec(t, nil, "config"); code != 2 {
		t.Fatalf("config without check: exit %d", code)
	}
	if code, out, _ = exec(t, nil, "version", "-bogus", "-json"); code != 2 || decode(t, out).Error.Code != contract.CodeInvalidRequest {
		t.Fatalf("bad flag: %d %q", code, out)
	}
}

func TestHandlerServesEveryPublicRoute(t *testing.T) {
	srv := httptest.NewServer(newHandler())
	defer srv.Close()
	for _, r := range contract.Routes() {
		if !r.Public {
			continue
		}
		req, err := http.NewRequestWithContext(context.Background(), r.Method, srv.URL+r.Pattern, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s %s: status %d", r.Method, r.Pattern, resp.StatusCode)
		}
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/nope", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body contract.ErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || resp.StatusCode != 404 || body.Code != contract.CodeNotFound {
		t.Fatalf("404 body: %d %+v %v", resp.StatusCode, body, err)
	}
}

func TestServeStartsAndShutsDownGracefully(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pr := &syncBuf{}
	done := make(chan int, 1)
	go func() {
		done <- runCtx(ctx, []string{"serve", "-json"}, lookup(map[string]string{
			"METERWISE_LISTEN": "127.0.0.1:0", "METERWISE_DATABASE_URL": "postgres://u:p@127.0.0.1:5432/db",
		}), pr, &bytes.Buffer{})
	}()
	var addr string
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline) && addr == ""; time.Sleep(10 * time.Millisecond) {
		var r result
		if json.Unmarshal([]byte(strings.TrimSpace(pr.String())), &r) == nil && r.OK {
			addr, _ = r.Data["listening"].(string)
		}
	}
	if addr == "" {
		t.Fatal("server never reported its address")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: %v", err)
	}
	_ = resp.Body.Close()
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no graceful shutdown")
	}
}

func TestServeRejectsBadConfigAndTakenPort(t *testing.T) {
	if code, out, _ := exec(t, nil, "serve", "-json"); code != 1 || decode(t, out).Error.Code != contract.CodeConfigInvalid {
		t.Fatalf("missing config: %d %q", code, out)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	code, out, _ := exec(t, map[string]string{
		"METERWISE_LISTEN": ln.Addr().String(), "METERWISE_DATABASE_URL": "postgres://u:p@127.0.0.1:5432/db",
	}, "serve", "-json")
	if code != 1 || decode(t, out).Error.Code != contract.CodeInternal {
		t.Fatalf("taken port: %d %q", code, out)
	}
}

func TestLogLevel(t *testing.T) {
	if logLevel("debug") >= logLevel("error") || logLevel("garbage") != logLevel("info") {
		t.Fatal("log level mapping wrong")
	}
}
