package archtest

import (
	"slices"
	"sort"
	"testing"
)

const utilImport = `"` + module + `/internal/util"`

// utilTree is a handler file in internal/tenancy plus a comparison helper in internal/util, a
// package that is neither a role owner nor the handler: the role crosses a package boundary.
func utilTree(helper, handler string) map[string]string {
	t := roleTree("internal/tenancy/x.go", roleFile(roleImport+"\n"+utilImport, handler))
	t["internal/util/same.go"] = "package util\n" + helper + "\n"
	return t
}

// roleGaps are the shapes the first go/types rule missed (verifier round 2). Each must be H11.
var roleGaps = map[string]map[string]string{
	"1 repo helper in the same package": roleTree("internal/tenancy/x.go", roleFile("",
		"func same(a, b string) bool { return a == b }\nfunc f(p P) bool { return same(string(p.Role), \"admin\") }")),
	"1 repo helper in another package": utilTree("func Same(a, b string) bool { return a == b }",
		"func f(p P) bool { return util.Same(string(p.Role), \"admin\") }"),
	"1 helper forwards to a helper": utilTree("func inner(a, b string) bool { return a == b }\nfunc Same(a, b string) bool { return inner(a, b) }",
		"func f(p P) bool { return util.Same(string(p.Role), \"admin\") }"),
	"2 generic comparable helper": roleTree("internal/tenancy/x.go", roleFile("",
		"func eq[T comparable](a, b T) bool { return a == b }\nfunc f(p P) bool { return eq(p.Role, contract.RoleAdmin) }")),
	"2 generic helper, explicit instantiation": roleTree("internal/tenancy/x.go", roleFile("",
		"func eq[T comparable](a, b T) bool { return a == b }\nfunc f(p P) bool { return eq[contract.Role](p.Role, contract.RoleAdmin) }")),
	"2 generic helper in another package": utilTree("func Eq[T comparable](a, b T) bool { return a == b }",
		"func f(p P) bool { return util.Eq(p.Role, contract.RoleAdmin) }"),
	"3 struct equality on the principal": roleTree("internal/tenancy/x.go", roleFile("",
		"func f(p P) bool { return p == P{Role: contract.RoleAdmin} }")),
	"3 struct equality through a nested struct": roleTree("internal/tenancy/x.go", roleFile("",
		"func f(a, b S) bool { return a != b }")),
	"4 role copied into a field, field compared": roleTree("internal/tenancy/x.go", roleFile("",
		"type holder struct{ r string }\nfunc f(p P, o string) bool { var x holder; x.r = string(p.Role); return x.r == o }")),
	"4 role in a composite literal field": roleTree("internal/tenancy/x.go", roleFile("",
		"type holder2 struct{ r string }\nfunc f(p P, o string) bool { x := holder2{r: string(p.Role)}; return x.r == o }")),
	"5 closure captures the role": roleTree("internal/tenancy/x.go", roleFile("",
		"func f(p P, o string) bool { r := string(p.Role); return func() bool { return r == o }() }")),
	"5 closure takes the role as a parameter": roleTree("internal/tenancy/x.go", roleFile("",
		"func f(p P, o string) bool { return func(r string) bool { return r == o }(string(p.Role)) }")),
	"5 closure bound to a variable": roleTree("internal/tenancy/x.go", roleFile("",
		"func f(p P, o string) bool { g := func(r string) bool { return r == o }; return g(string(p.Role)) }")),
	"6 role through a channel": roleTree("internal/tenancy/x.go", roleFile("",
		"func f(p P, o string) bool { ch := make(chan string, 1); ch <- string(p.Role); v := <-ch; return v == o }")),
	"6 role through a channel, ranged": roleTree("internal/tenancy/x.go", roleFile("",
		"func f(p P, o string) bool { ch := make(chan string, 1); ch <- string(p.Role); close(ch); for v := range ch { if v == o { return true } }; return false }")),
	"7 two plain-string Role fields": roleTree("internal/tenancy/x.go", roleFile("",
		"type U struct{ Role string }\nfunc f(a, b U) bool { return a.Role == b.Role }")),
}

func TestSeededRoleGapsGoRed(t *testing.T) {
	var names []string
	for n := range roleGaps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if r := rules(scanInlineRoleChecks(roleGaps[name])); !slices.Equal(r, []string{"H11"}) {
			t.Errorf("%s: not caught (%v)", name, r)
		}
	}
}

// The neighbours of the gaps stay legal: a role handed to code that does not decide with it.
func TestRoleGapNeighboursStayGreen(t *testing.T) {
	good := utilTree("func Show(a string) string { return a + \"!\" }\nfunc Same(a, b string) bool { return a == b }",
		`type dto struct{ Role string }
func keep[T any](a, b T) T { return a }
func ids(a, b string) bool { return a == b }
func f(p P, id, other string) (dto, bool) {
	_ = keep(p.Role, contract.RoleMember)
	_ = util.Show(string(p.Role))
	_ = util.Same(id, other)
	ch := make(chan contract.Role, 1)
	ch <- p.Role
	got := <-ch
	d := dto{Role: string(got)}
	_ = func(r string) string { return r }(string(p.Role))
	return d, ids(id, other)
}`)
	if got := scanInlineRoleChecks(good); len(got) != 0 {
		t.Fatalf("legal uses flagged: %v", got)
	}
	// Owners may call each other and compare in their own helpers.
	owner := map[string]string{
		"internal/contract/ports.go": roleStub,
		"internal/access/x.go":       "package access\nimport \"" + cn + "\"\nfunc eq[T comparable](a, b T) bool { return a == b }\nfunc IsAdmin(r contract.Role) bool { return eq(r, contract.RoleAdmin) }\n",
		"internal/tenancy/y.go":      "package tenancy\nimport (\n\"" + cn + "\"\n\"" + module + "/internal/access\"\n)\nfunc f(r contract.Role) bool { return access.IsAdmin(r) }\n",
	}
	if got := scanInlineRoleChecks(owner); len(got) != 0 {
		t.Fatalf("sanctioned access.IsAdmin flagged: %v", got)
	}
}
