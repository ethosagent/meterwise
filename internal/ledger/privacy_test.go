package ledger_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/ledger"
)

const (
	userSentinel   = "SENTINEL-USER-MESSAGE-4f2a"
	systemSentinel = "SYSTEM-PROMPT-TEXT"
	keySentinel    = "sk-secret-not-real"
)

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

func TestPreviewOnlyWhenCaptureOnAndPrivacyModeOff(t *testing.T) {
	long := userSentinel + strings.Repeat("é", 400)
	tests := []struct {
		name    string
		policy  *tollgate.Policy
		global  bool
		calls   int
		want    bool
		wantLen int
	}{
		{"no policy", nil, false, 1, false, 0},
		{"capture off", &tollgate.Policy{}, false, 1, false, 0},
		{"capture on, global privacy off", &tollgate.Policy{Capture: true}, false, 1, true, 200},
		{"capture on but global privacy mode on", &tollgate.Policy{Capture: true}, true, 1, false, 0},
		{"capture on but workspace forces privacy on", &tollgate.Policy{Capture: true, PrivacyMode: yes()}, false, 1, false, 0},
		{"capture on, workspace privacy explicitly off, global off", &tollgate.Policy{Capture: true, PrivacyMode: no()}, false, 1, true, 200},
		{"capture on, workspace privacy off cannot beat global on", &tollgate.Policy{Capture: true, PrivacyMode: no()}, true, 1, false, 0},
		{"only the first call of a session carries it", &tollgate.Policy{Capture: true}, false, 2, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := &fakeWriter{}
			h := newHook(t, w, func(o *ledger.Options) { o.PrivacyMode = tc.global })
			complete(t, h, directCall("p1", long, tc.policy, tc.calls))
			if err := h.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := w.snapshot()[0].Preview
			if tc.want != (got != "") || len([]rune(got)) != tc.wantLen || (tc.want && !strings.HasPrefix(got, userSentinel)) {
				t.Fatalf("preview = %q (%d chars), want present=%v len=%d", got, len([]rune(got)), tc.want, tc.wantLen)
			}
			if strings.Contains(got, systemSentinel) {
				t.Fatal("the system prompt leaked into the preview")
			}
		})
	}
}
