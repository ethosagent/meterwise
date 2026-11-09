package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A handler reached without the principal middleware must refuse, not act for an empty user.
func TestHandlersRefuseARequestWithNoPrincipal(t *testing.T) {
	s := &server{Deps: Deps{Prefs: NewMemoryPreferences()}}
	for name, h := range map[string]http.HandlerFunc{"get": s.getPreferences, "patch": s.patchPreferences} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodPost, "/api/me/preferences", strings.NewReader(`{"theme":"dark"}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a principal: %d %s, want 401", name, rec.Code, rec.Body)
		}
	}
}
