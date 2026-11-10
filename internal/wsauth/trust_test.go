package wsauth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ethosagent/meterwise/internal/wsauth"
)

// Front is the one place that validates the id (wsauth.Parse); the authenticator hands the id it
// finds in the context to the directory as is and does not judge its shape a second time.
func TestAuthenticateTrustsTheIDFrontStashed(t *testing.T) {
	for _, tc := range []struct {
		name, ctxID, path, wantID string
	}{
		{"stashed id wins over the path", curID, "/openai/v1/x", curID},
		{"no stashed id: the path's id", "", "/w/" + oldID + "/x", oldID},
		{"stashed id is not re-validated", "not-an-id", "/openai/v1/x", "not-an-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &recDir{hit: true, err: errors.New("stop")}
			ctx := context.Background()
			if tc.ctxID != "" {
				ctx = wsauth.WithPublicID(ctx, tc.ctxID)
			}
			_, _ = wsauth.New(d, 0).Authenticate(ctx, authReq(tc.path, "198.51.100.1:1"))
			if d.id != tc.wantID {
				t.Fatalf("directory asked for %q, want %q", d.id, tc.wantID)
			}
		})
	}
}
