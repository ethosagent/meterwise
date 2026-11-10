package api_test

import (
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// One limit convention for every paged list: an explicit limit outside 1..MaxPageLimit is a 422 that
// carries the shared message, 0 included (omit the parameter for the default).
func TestPageLimitConventionIsSharedByAuditAndSessions(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	paths := map[string]string{"audit": "/api/audit-log?limit=", "sessions": "/api/workspaces/" + ws + "/sessions?limit="}
	for name, base := range paths {
		for _, lim := range []string{"0", "-1", "abc", "201"} {
			body := string(s.must(t, admin.tok, "GET", base+lim, "", 422))
			if !strings.Contains(body, contract.LimitMessage) {
				t.Errorf("%s limit=%s: body %s lacks %q", name, lim, body, contract.LimitMessage)
			}
		}
		s.must(t, admin.tok, "GET", base+"1", "", 200)
		s.must(t, admin.tok, "GET", base+"200", "", 200)
	}
}
