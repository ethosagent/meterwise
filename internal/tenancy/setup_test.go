package tenancy_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

func TestSnippetsFillTheURLAndOpencodeIsValidJSON(t *testing.T) {
	const u = "https://proxy.example.test/w/AAAAAAAAAAAAAAAAAAAAAA"
	sn := tenancy.SnippetRenderer{}.Render(u)
	for name, s := range map[string]string{"openai": sn.OpenaiEnv, "anthropic": sn.AnthropicEnv, "opencode": sn.OpencodeJSON, "claude": sn.ClaudeCodeEnv, "curl": sn.Curl} {
		if !strings.Contains(s, u) {
			t.Errorf("%s snippet lacks the URL:\n%s", name, s)
		}
	}
	if !strings.Contains(sn.OpenaiEnv, "OPENAI_BASE_URL="+u+"/v1") || !strings.Contains(sn.AnthropicEnv, "ANTHROPIC_BASE_URL="+u+"\n") || !strings.Contains(sn.ClaudeCodeEnv, "ANTHROPIC_BASE_URL="+u) {
		t.Fatalf("env snippets: %+v", sn)
	}
	for _, s := range []string{sn.OpenaiEnv, sn.AnthropicEnv, sn.ClaudeCodeEnv, sn.Curl} {
		if !strings.Contains(s, "keep using your own provider key") {
			t.Errorf("missing the provider-key note:\n%s", s)
		}
	}
	var doc struct {
		Provider map[string]struct {
			NPM     string `json:"npm"`
			Options struct {
				BaseURL string `json:"baseURL"`
				APIKey  string `json:"apiKey"`
			} `json:"options"`
			Models map[string]any `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal([]byte(sn.OpencodeJSON), &doc); err != nil {
		t.Fatalf("opencode json: %v\n%s", err, sn.OpencodeJSON)
	}
	p := doc.Provider["meterwise"]
	if p.NPM == "" || p.Options.BaseURL != u+"/v1" || len(p.Models) == 0 || strings.Contains(p.Options.APIKey, "sk-") {
		t.Fatalf("provider block: %+v", p)
	}
	if sn != (tenancy.SnippetRenderer{}).Render(u) {
		t.Fatal("render is not deterministic")
	}
}

func TestNormalizeCIDRs(t *testing.T) {
	got, err := tenancy.NormalizeCIDRs([]string{"10.1.2.3/8", "203.0.113.7", "2001:db8::1", "::ffff:192.0.2.1", "10.0.0.0/8"})
	want := []string{"10.0.0.0/8", "203.0.113.7/32", "2001:db8::1/128", "192.0.2.1/32"}
	if err != nil || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v %v, want %v", got, err, want)
	}
	if got, err := tenancy.NormalizeCIDRs(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty: %#v %v", got, err)
	}
	for name, in := range map[string][]string{
		"junk": {"10.0.0.0/8", "nope", "1.2.3.4/33"}, "ipv6 zone": {"fe80::1%eth0"}, "ipv6 zone with mask": {"fe80::1%eth0/64"}, "empty entry": {""}, "too many": make([]string, tenancy.MaxAllowedCIDRs+1),
	} {
		_, err := tenancy.NormalizeCIDRs(in)
		e := contract.AsError(err)
		if e.Code != contract.CodeValidationFailed {
			t.Errorf("%s: %v", name, err)
		}
		if name == "junk" && len(e.Details) != 2 {
			t.Errorf("every bad entry must be listed: %v", e.Details)
		}
	}
}

func TestSetupStatusAndAllowedCIDRsThroughTheService(t *testing.T) {
	_, svc, db, _ := newDir(t)
	ctx := context.Background()
	ws, _ := svc.CreateWorkspace(ctx, principal(), "Alpha")
	su, err := svc.Setup(ctx, principal(), ws.ID, "https://h/w/")
	if err != nil || su.Status.LastCallAt != nil || su.Status.FirstCallAt != nil || su.Status.CallsToday != 0 || su.Snippets.Curl == "" || su.ProxyURL != "https://h/w/"+ws.PublicID || !strings.Contains(su.Snippets.OpenaiEnv, su.ProxyURL) {
		t.Fatalf("fresh: %+v %v", su, err)
	}
	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if _, err := db.Pool().Exec(ctx, `UPDATE workspaces SET last_call_at = $1 WHERE id = $2`, at, ws.ID); err != nil {
		t.Fatal(err)
	}
	if su, _ = svc.Setup(ctx, principal(), ws.ID, "x/"); su.Status.LastCallAt == nil || *su.Status.LastCallAt != "2026-05-01T12:00:00Z" {
		t.Fatalf("last call: %+v", su.Status)
	}
	if _, err := svc.Setup(ctx, principal(), "00000000-0000-0000-0000-000000000fff", "x/"); contract.AsError(err).Code != contract.CodeNotFound {
		t.Fatalf("unknown: %v", err)
	}
	got, err := svc.SetAllowedCIDRs(ctx, principal(), ws.ID, []string{"198.51.100.4"})
	if err != nil || len(got) != 1 || got[0] != "198.51.100.4/32" {
		t.Fatalf("cidrs: %v %v", got, err)
	}
	// the directory sees it at once (cache busted)
	if w, _ := svc.Directory().ByPublicID(ctx, ws.PublicID); len(w.AllowedClientCIDRs) != 1 {
		t.Fatalf("directory: %+v", w.AllowedClientCIDRs)
	}
	if _, err := svc.SetAllowedCIDRs(ctx, principal(), ws.ID, []string{"bad"}); contract.AsError(err).Code != contract.CodeValidationFailed {
		t.Fatalf("bad: %v", err)
	}
}
