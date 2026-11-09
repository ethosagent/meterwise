package archtest

import (
	"slices"
	"testing"
)

func TestSeededInlineRoleChecksGoRed(t *testing.T) {
	for name, body := range map[string]string{
		"field equality":        "package api\nfunc f(p P) bool { return p.Role == contract.RoleAdmin }\n",
		"field inequality":      "package api\nfunc f(p P) bool { return p.Role != contract.RoleMember }\n",
		"constant on the left":  "package api\nfunc f(r contract.Role) bool { return contract.RoleAdmin == r }\n",
		"constant on the right": "package api\nfunc f(r contract.Role) bool { return r == contract.RoleAdmin }\n",
		"switch on the role":    "package api\nfunc f(s S) { switch s.Principal.Role { } }\n",
		"case on a constant":    "package api\nfunc f(r contract.Role) { switch r {\ncase contract.RoleAdmin: } }\n",
		"string literal":        "package api\nfunc f(r string) bool { return r == \"admin\" }\n",
		"the removed Allows":    "package api\nvar _ = contract.Allows(r, p)\n",
		"in a command":          "package main\nfunc f(p P) bool { return p.Role == 1 }\n",
	} {
		got := scanInlineRoleChecks(map[string]string{"internal/tenancy/x.go": body})
		if r := rules(got); !slices.Equal(r, []string{"H11"}) {
			t.Errorf("%s: %v", name, r)
		}
	}
}

func TestLegalRoleUsesStayGreen(t *testing.T) {
	good := map[string]string{
		// the owners compare roles
		"internal/access/evaluator.go": "package access\nfunc f(r contract.Role) bool { return r == contract.RoleAdmin }\n",
		"internal/contract/ports.go":   "package contract\nfunc (r Role) Valid() bool { return r == RoleAdmin || r == RoleMember }\n",
		"internal/store/members.go":    "package store\nfunc f(r string) bool { return r == string(contract.RoleAdmin) && p.Role != 1 }\n",
		// tests may assert on roles
		"internal/api/x_test.go": "package api\nvar _ = me.Role == contract.RoleAdmin\n",
		// passing or storing a role is not a decision
		"internal/tenancy/ok.go": "package tenancy\nfunc f() { s.invite(ctx, \"\", e, contract.RoleAdmin); x := contract.Principal{Role: r}; _ = x; if !role.Valid() {} }\n",
		// prose and comments
		"internal/api/prose.go": "package api\n// the old check was p.Role == contract.RoleAdmin\n/* r == \"admin\" */\nvar x = 1\n",
	}
	if got := scanInlineRoleChecks(good); len(got) != 0 {
		t.Fatalf("legal uses flagged: %v", got)
	}
}
