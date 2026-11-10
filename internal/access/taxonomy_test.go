package access_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/contract"
)

func TestTaxonomyIsThePlanSection4Table(t *testing.T) {
	want := map[string]string{
		"workspace": "workspace:read,write,manage", "workspace_url": "workspace:read,manage", "session": "workspace:read",
		"monitor_config": "workspace:read,write", "savings": "workspace:read", "member": "org:read,manage", "audit_log": "org:read",
	}
	got := map[string]string{}
	for _, r := range access.Taxonomy() {
		got[r.Type] = string(r.Scope) + ":" + strings.Join(r.Actions, ",")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	// the copy cannot be used to edit the taxonomy
	access.Taxonomy()[0].Actions[0] = "hacked"
	if !access.Valid("audit_log", "read") || access.Valid("audit_log", "hacked") {
		t.Fatal("Taxonomy returned a live slice")
	}
}

func TestValidAndParse(t *testing.T) {
	for _, bad := range []string{"", "workspace", "workspace:", ":read", "workspace:delete", "nope:read", "member:write", "WORKSPACE:read", "workspace:read:x"} {
		if _, _, ok := access.Parse(contract.Permission(bad)); ok {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
	if rt, a, ok := access.Parse(contract.PermMonitorConfigWrite); !ok || rt != "monitor_config" || a != "write" {
		t.Fatal("Parse")
	}
	if s, ok := access.ScopeOf("member"); !ok || s != access.ScopeOrg {
		t.Fatal("scope")
	}
	if _, ok := access.ScopeOf("nope"); ok {
		t.Fatal("unknown scope")
	}
}

// Every route permission must be in the taxonomy (contract cannot import access, H0, so the
// route table spells them as strings and this test is the link), and a workspace-scoped one must
// say which path parameter names the workspace.
func TestEveryRoutePermissionIsInTheTaxonomy(t *testing.T) {
	classes := map[contract.Permission]bool{contract.PermSelf: true, contract.PermMember: true, contract.PermAdmin: true}
	for _, r := range contract.Routes() {
		if r.Public || classes[r.Permission] {
			if r.WorkspaceParam != "" {
				t.Errorf("%s %s: WorkspaceParam on a %q route", r.Method, r.Pattern, r.Permission)
			}
			continue
		}
		rt, _, ok := access.Parse(r.Permission)
		if !ok {
			t.Errorf("%s %s: permission %q is not in the taxonomy", r.Method, r.Pattern, r.Permission)
			continue
		}
		scope, _ := access.ScopeOf(rt)
		if (scope == access.ScopeWorkspace) != (r.WorkspaceParam != "" || r.WorkspaceQuery != "") {
			t.Errorf("%s %s: %s is %s-scoped but WorkspaceParam=%q WorkspaceQuery=%q", r.Method, r.Pattern, r.Permission, scope, r.WorkspaceParam, r.WorkspaceQuery)
		}
	}
}

func TestEveryPermissionConstantIsInTheTaxonomy(t *testing.T) {
	for _, p := range []contract.Permission{
		contract.PermWorkspaceRead, contract.PermWorkspaceWrite, contract.PermWorkspaceManage, contract.PermWorkspaceURLRead,
		contract.PermWorkspaceURLManage, contract.PermSessionRead, contract.PermMonitorConfigRead, contract.PermMonitorConfigWrite,
		contract.PermSavingsRead, contract.PermMemberRead, contract.PermMemberManage, contract.PermAuditLogRead,
	} {
		if _, _, ok := access.Parse(p); !ok {
			t.Errorf("%s is not in the taxonomy", p)
		}
	}
}

func TestWorkspaceReadSeedIsTheReadActionOfEveryWorkspaceScopedResource(t *testing.T) {
	var got []string
	for _, g := range access.WorkspaceReadSeed() {
		got = append(got, g.ResourceType+":"+g.Action)
	}
	want := []string{"monitor_config:read", "savings:read", "session:read", "workspace:read", "workspace_url:read"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestValidateGrants(t *testing.T) {
	ws := wsA
	g := func(w *string, rt, a string) contract.Grant {
		return contract.Grant{WorkspaceID: w, ResourceType: rt, Action: a}
	}
	for name, tc := range map[string]struct {
		in        []contract.Grant
		orgDef    bool
		wantProbs []string
	}{
		"fine":                          {in: []contract.Grant{g(&ws, "workspace", "read"), g(nil, "member", "read")}},
		"empty":                         {in: nil},
		"unknown type":                  {in: []contract.Grant{g(nil, "nope", "read")}, wantProbs: []string{`unknown resourceType "nope"`}},
		"unknown action":                {in: []contract.Grant{g(&ws, "workspace", "delete")}, wantProbs: []string{`unknown action "delete"`}},
		"org scoped with a workspace":   {in: []contract.Grant{g(&ws, "member", "read")}, wantProbs: []string{"org-scoped"}},
		"workspace scoped without one":  {in: []contract.Grant{g(nil, "workspace", "read")}, wantProbs: []string{"workspaceId is required"}},
		"null workspace ok in defaults": {in: []contract.Grant{g(nil, "workspace", "read")}, orgDef: true},
		"duplicate":                     {in: []contract.Grant{g(&ws, "workspace", "read"), g(&ws, "workspace", "read")}, wantProbs: []string{"duplicate"}},
		"every problem is listed":       {in: []contract.Grant{g(nil, "x", "y"), g(nil, "workspace", "read")}, wantProbs: []string{"grants[0]", "grants[1]"}},
	} {
		t.Run(name, func(t *testing.T) {
			err := access.ValidateGrants(tc.in, tc.orgDef)
			if len(tc.wantProbs) == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			e := contract.AsError(err)
			if e.Code != contract.CodeValidationFailed {
				t.Fatalf("%v", err)
			}
			all := strings.Join(e.Details, "|")
			for _, w := range tc.wantProbs {
				if !strings.Contains(all, w) {
					t.Errorf("%q missing from %q", w, all)
				}
			}
		})
	}
}
