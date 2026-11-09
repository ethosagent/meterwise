package contract_test

import (
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Non-ASCII characters that Unicode case folding maps onto ASCII.
var foldingImpostors = map[string]string{
	"kelvin sign (U+212A) for k":         "Kate@corp.test",
	"dotted capital I (U+0130) for i":    "mİke@corp.test",
	"kelvin sign in the domain":          "ceo@Kpmg.test",
	"dotted capital I in the domain":     "ceo@İbm.test",
	"long s (U+017F) for s":              "ſam@corp.test",
	"non-ASCII with surrounding spaces":  "  Kate@corp.test ",
	"no-break space before the address":  " kate@corp.test",
	"no-break space after the address":   "kate@corp.test ",
	"ideographic space around":           "　kate@corp.test　",
	"em space before the address":        " kate@corp.test",
	"zero width no-break space (U+FEFF)": "\ufeffkate@corp.test",
}

func TestNormalizeEmailRefusesNonASCIIBeforeFolding(t *testing.T) {
	for name, raw := range foldingImpostors {
		t.Run(name, func(t *testing.T) {
			if got, ok := contract.NormalizeEmail(raw); ok {
				t.Fatalf("NormalizeEmail(%q) = %q, true", raw, got)
			}
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	good := map[string]string{"KATE@Corp.TEST": "kate@corp.test", "ada@example.test": "ada@example.test"}
	for raw, want := range good {
		if got, ok := contract.NormalizeEmail(raw); !ok || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v", raw, got, ok)
		}
	}
	bad := []string{"", " ada@example.test", "ada@example.test ", "ada@example.test\n", "nope", "a@b@c",
		strings.Repeat("a", 260) + "@x.test", "Ada <ada@example.test>", "<ada@example.test>", "ada @example.test"}
	for _, raw := range bad {
		if got, ok := contract.NormalizeEmail(raw); ok {
			t.Errorf("NormalizeEmail(%q) = %q, true; want refused (it never trims)", raw, got)
		}
	}
}

func TestNotInvitedIsTheRegisteredError(t *testing.T) {
	e := contract.NotInvited()
	if e.Code != contract.CodeNotInvited || e.Status() != 403 {
		t.Fatalf("%+v", e)
	}
}

func TestGrantHelpers(t *testing.T) {
	row := contract.GrantRow{GroupID: "g", WorkspaceID: "w1", ResourceType: "workspace", Action: "read"}
	g := row.ToGrant()
	if g.WorkspaceID == nil || *g.WorkspaceID != "w1" || g.ResourceType != "workspace" || g.Action != "read" {
		t.Fatalf("ToGrant: %+v", g)
	}
	if g.WorkspaceOrEmpty() != "w1" {
		t.Fatal("WorkspaceOrEmpty")
	}
	org := contract.GrantRow{ResourceType: "member", Action: "read"}.ToGrant()
	if org.WorkspaceID != nil || org.WorkspaceOrEmpty() != "" {
		t.Fatalf("an org-wide row has no workspace: %+v", org)
	}
	if g.Key() == org.Key() || g.Key() != row.ToGrant().Key() || org.Key() != "member\x00read\x00" {
		t.Fatalf("Key: %q %q", g.Key(), org.Key())
	}
}

func TestValidNameAndInvalid(t *testing.T) {
	if n, err := contract.ValidName("  Alpha  "); err != nil || n != "Alpha" {
		t.Fatalf("%q %v", n, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", 81), "tab\tname", "ctl\x01"} {
		_, err := contract.ValidName(bad)
		e := contract.AsError(err)
		if e.Code != contract.CodeValidationFailed || len(e.Details) != 1 || !strings.HasPrefix(e.Details[0], "name: ") {
			t.Errorf("ValidName(%q) = %v", bad, err)
		}
	}
	if _, err := contract.ValidName(strings.Repeat("é", 80)); err != nil {
		t.Errorf("80 runes of 2 bytes each are fine: %v", err)
	}
	if e := contract.Invalid("role", "bad"); e.Code != contract.CodeValidationFailed || e.Details[0] != "role: bad" {
		t.Fatalf("%+v", e)
	}
}
