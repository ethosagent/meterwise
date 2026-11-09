package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

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
	good := map[string]string{"DATABASE_URL": "postgres://u:p@127.0.0.1:1/db?sslmode=disable", "APP_ENV": "dev"}
	tests := []struct {
		args     []string
		wantOK   bool
		wantCode contract.Code
		exit     int
	}{
		{[]string{"version", "-json"}, true, "", 0},
		{[]string{"config", "check", "-json"}, true, "", 0},
		{[]string{"migrate", "status", "-json"}, false, contract.CodeUnavailable, 1},
		{[]string{"migrate", "up", "-json"}, false, contract.CodeUnavailable, 1},
		{[]string{"bootstrap-admin", "-json"}, false, contract.CodeInvalidRequest, 2},
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
		"LISTEN_PROXY": "bad", "LOG_LEVEL": "loud", "APP_ENV": "moon",
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
	for _, dsn := range []string{
		"postgres://u:hunter2@127.0.0.1:5432/db",
		"postgres://u@127.0.0.1:5432/db?password=hunter2",
		"postgres://u@127.0.0.1:5432/db?password=hunt%65r2&sslmode=disable",
	} {
		kv := map[string]string{"DATABASE_URL": dsn, "APP_ENV": "dev"}
		_, out, _ := exec(t, kv, "config", "check", "-json")
		if strings.Contains(out, "hunter2") || strings.Contains(out, "hunt%65r2") {
			t.Fatalf("%s: password leaked: %s", dsn, out)
		}
		_, out, _ = exec(t, kv, "config", "check")
		if strings.Contains(out, "hunter2") || strings.Contains(out, "hunt%65r2") || !strings.Contains(out, "LISTEN_PROXY=:8080") {
			t.Fatalf("%s: text output wrong: %s", dsn, out)
		}
	}
}

func TestConfigCheckInProdListsTheMissingIdentitySettings(t *testing.T) {
	code, out, _ := exec(t, map[string]string{"DATABASE_URL": "postgres://u:p@127.0.0.1:5432/db"}, "config", "check", "-json")
	r := decode(t, out)
	if code != 1 || r.Error.Code != contract.CodeConfigInvalid || len(r.Error.Details) != 4 {
		t.Fatalf("%d %s", code, out)
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
	code, _, errb := exec(t, nil, "bootstrap-admin")
	if code != 2 || !strings.Contains(errb, "usage: meterwised bootstrap-admin") {
		t.Fatalf("bootstrap-admin text: %d %q", code, errb)
	}
	if code, _, _ = exec(t, map[string]string{"DATABASE_URL": "postgres://u:p@127.0.0.1:5432/db"}, "migrate", "sideways"); code != 2 {
		t.Fatalf("migrate with a bad action: exit %d", code)
	}
	if code, _, _ = exec(t, nil, "config"); code != 2 {
		t.Fatalf("config without check: exit %d", code)
	}
	if code, out, _ = exec(t, nil, "version", "-bogus", "-json"); code != 2 || decode(t, out).Error.Code != contract.CodeInvalidRequest {
		t.Fatalf("bad flag: %d %q", code, out)
	}
}

func TestLogLevel(t *testing.T) {
	if logLevel("debug") >= logLevel("error") || logLevel("garbage") != logLevel("info") {
		t.Fatal("log level mapping wrong")
	}
}
