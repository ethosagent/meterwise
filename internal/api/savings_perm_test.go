package api_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// permCases is web/src/test/fixtures/savingsPermCases.json, the file the web hasPermission test reads too: the
// gateway and the UI must agree on every (snapshot, workspace, route) row.
type permCases struct {
	Cases []struct {
		Name   string `json:"name"`
		Admin  bool   `json:"admin"`
		Grants []struct {
			Workspace    *string `json:"workspace"`
			ResourceType string  `json:"resourceType"`
			Action       string  `json:"action"`
		} `json:"grants"`
		Workspace string `json:"workspace"`
		Route     string `json:"route"`
		Allowed   bool   `json:"allowed"`
	} `json:"cases"`
}

func TestGatewayAgreesWithTheSharedPermissionFixture(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/test/fixtures/savingsPermCases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx permCases
	if err := json.Unmarshal(raw, &fx); err != nil || len(fx.Cases) == 0 {
		t.Fatalf("fixture: %v (%d cases)", err, len(fx.Cases))
	}
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	_, a := s.defaultWorkspace(t, admin.tok)
	ids := map[string]string{"A": a, "B": s.newWorkspace(t, admin.tok, "B")}
	for _, c := range fx.Cases {
		t.Run(c.Name, func(t *testing.T) {
			tok := m.tok
			if c.Admin {
				tok = admin.tok
			} else {
				gs := make([]contract.Grant, 0, len(c.Grants))
				for _, gr := range c.Grants {
					ws := ""
					if gr.Workspace != nil {
						ws = ids[*gr.Workspace]
					}
					gs = append(gs, g(ws, gr.ResourceType, gr.Action))
				}
				s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(gs...), 200)
			}
			code, b := s.call(t, "GET", "/api/workspaces/"+ids[c.Workspace]+"/"+c.Route, tok, "")
			if got := code == 200; got != c.Allowed || (!c.Allowed && code != 403) {
				t.Fatalf("GET %s in %s: %d %s, allowed=%v", c.Route, c.Workspace, code, b, c.Allowed)
			}
		})
	}
}
