package wsauth_test

import (
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

func TestResolveValidatesTheTenantUUIDsOnce(t *testing.T) {
	for _, tc := range []struct {
		name, ws, org string
		want          string
	}{
		{"both canonical", wsID, orgID, "true"},
		{"upper case is not canonical", "AAAAAAAA-1111-1111-1111-111111111111", orgID, ""},
		{"workspace not a uuid", "dev-ws", orgID, ""},
		{"org not a uuid", wsID, "org", ""},
		{"org missing", wsID, "", ""},
		{"workspace missing", "", orgID, ""},
		{"wrong length", wsID + "0", orgID, ""},
		{"misplaced dash", "1111111-11111-1111-1111-111111111111", orgID, ""},
		{"non-hex digit", "1111111g-1111-1111-1111-111111111111", orgID, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := wsauth.Resolve(contract.Workspace{ID: tc.ws, OrgID: tc.org}).(*wsauth.Resolved)
			if got := res.Tenant.Attrs[wsauth.AttrTenantOK]; got != tc.want {
				t.Fatalf("%s = %q, want %q", wsauth.AttrTenantOK, got, tc.want)
			}
		})
	}
}
