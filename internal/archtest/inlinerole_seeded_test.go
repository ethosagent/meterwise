package archtest

import (
	"slices"
	"sort"
	"testing"
	"time"
)

const roleStub = `package contract

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

func (r Role) Valid() bool { return r == RoleAdmin || r == RoleMember }
`

const roleImport = `"` + cn + `"`

// roleFile is a compilable-enough source file in package api: the stub contract, and the shapes
// the samples use (a principal with a Role, a session holding one). Unresolved standard-library
// imports are fine; the scan only needs contract.
func roleFile(imports, body string) string {
	if imports == "" {
		imports = roleImport
	}
	return "package api\nimport (\n" + imports + "\n)\n" +
		"type P struct{ Role contract.Role }\n" +
		"type S struct{ Principal P }\n" +
		"func (p P) GetRole() contract.Role { return p.Role }\n" + body + "\n"
}

func roleTree(rel, src string) map[string]string {
	return map[string]string{"internal/contract/ports.go": roleStub, rel: src}
}

const stdImports = "\"fmt\"\n\"strings\"\n\"slices\"\n\"bytes\"\n" + roleImport

// roleEvasions are the inline role decisions a line-based scan missed (reviewer sample set, every
// one compiles with its imports), and the baselines it did catch. Each must be H11 now.
var roleEvasions = map[string]string{
	"baseline p.Role == contract.RoleAdmin":    roleFile("", "func f(p P) bool { return p.Role == contract.RoleAdmin }"),
	"import alias r == c.RoleAdmin":            roleFile("c "+roleImport, "func f(r c.Role) bool { return r == c.RoleAdmin }"),
	"dot import":                               "package api\nimport . " + roleImport + "\nfunc f(r Role) bool { return r == RoleAdmin }\n",
	"local const alias":                        roleFile("", "const adm = contract.RoleAdmin\nfunc f(r contract.Role) bool { return r == adm }"),
	"package var alias":                        roleFile("", "var adm = contract.RoleAdmin\nfunc f(r contract.Role) bool { return adm != r }"),
	"role via literal cast":                    roleFile("", `func f(r contract.Role) bool { return r == contract.Role("admin") }`),
	"switch case string literal":               roleFile("", `func f(r contract.Role) bool { switch string(r) { case "admin": return true }; return false }`),
	"switch on local, case alias":              roleFile("", "var adm = contract.RoleAdmin\nfunc f(r contract.Role) bool { switch r { case adm: return true }; return false }"),
	"EqualFold":                                roleFile(stdImports, `func f(p P) bool { return strings.EqualFold(string(p.Role), "admin") }`),
	"HasPrefix":                                roleFile(stdImports, `func f(r contract.Role) bool { return strings.HasPrefix(string(r), "adm") }`),
	"slices.Contains":                          roleFile(stdImports, "func f(r contract.Role) bool { return slices.Contains([]contract.Role{contract.RoleAdmin}, r) }"),
	"map lookup":                               roleFile("", "var ok = map[contract.Role]bool{contract.RoleAdmin: true}\nfunc f(r contract.Role) bool { return ok[r] }"),
	"map lookup by string of a role":           roleFile("", "var ok = map[string]bool{\"x\": true}\nfunc f(r contract.Role) bool { return ok[string(r)] }"),
	"raw string literal":                       roleFile("", "func f(r string) bool { return r == `admin` }"),
	"hex escape string":                        roleFile("", `func f(r string) bool { return r == "\x61dmin" }`),
	"concatenated and named constant":          roleFile("", "const a = \"adm\"\nconst word = a + \"in\"\nfunc f(r string) bool { return r == word }"),
	"Sprint":                                   roleFile(stdImports, "func f(p P) bool { return fmt.Sprint(p.Role)[0] == 'a' }"),
	"ordering instead of equality":             roleFile("", `func f(r contract.Role) bool { return string(r) > "b" == false }`),
	"length of role":                           roleFile("", "func f(p P) bool { return len(p.Role) == 5 }"),
	"local var field copy":                     roleFile("", "var adm = contract.RoleAdmin\nfunc f(s S) bool { role := s.Principal.Role; return role == adm }"),
	"local string copy of a role":              roleFile("", `func f(s S) bool { name := string(s.Principal.Role); other := name; return other == "x" }`),
	"line comment in string hides the rest":    roleFile("", "func f(p P) bool { _ = \"//\"; return p.Role == contract.RoleAdmin }"),
	"url string before check":                  roleFile("", "func f(p P) bool { u := \"https://x\"; _ = u; return p.Role == contract.RoleAdmin }"),
	"block comment open in string":             roleFile("", "var a = \"/*\"\nfunc f(p P) bool { return p.Role == contract.RoleAdmin }\nvar b = \"*/\""),
	"role via method":                          roleFile("", "var adm = contract.RoleAdmin\nfunc f(p P) bool { return p.GetRole() == adm }"),
	"bytes":                                    roleFile(stdImports, "func f(p P) bool { return bytes.Equal([]byte(p.Role), []byte(\"admin\")) }"),
	"switch true case":                         roleFile("", "func f(p P) bool { switch { case p.Role == contract.RoleAdmin: return true }; return false }"),
	"parenthesised":                            roleFile("", "func f(p P) bool { return (p.Role) == (contract.RoleAdmin) }"),
	"role copied to a local, compared":         roleFile("", "func f(p P) bool { x := p.Role; return x == contract.RoleAdmin }"),
	"field inequality":                         roleFile("", "func f(p P) bool { return p.Role != contract.RoleMember }"),
	"constant on the left":                     roleFile("", "func f(r contract.Role) bool { return contract.RoleAdmin == r }"),
	"switch on the role":                       roleFile("", "func f(s S) { switch s.Principal.Role { } }"),
	"case on a constant":                       roleFile("", "func f(r contract.Role) { switch r {\ncase contract.RoleAdmin: } }"),
	"the removed Allows":                       roleFile("", "func f(r contract.Role) bool { return contract.Allows(r, \"x\") }"),
	"pointer to a role":                        roleFile("", "func f(r *contract.Role) bool { return *r == contract.RoleAdmin }"),
	"comparison inside a closure in a literal": roleFile("", "var g = func(p P) bool { return p.Role == contract.RoleAdmin }"),
}

func TestSeededInlineRoleChecksGoRed(t *testing.T) {
	var names []string
	for n := range roleEvasions {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		got := scanInlineRoleChecks(roleTree("internal/tenancy/x.go", roleEvasions[name]))
		if r := rules(got); !slices.Equal(r, []string{"H11"}) {
			t.Errorf("%s: %v", name, r)
		}
	}
	if len(roleEvasions) < 27 {
		t.Fatalf("the reviewer's 27 samples must stay seeded, have %d", len(roleEvasions))
	}
}

func TestSeededInlineRoleChecksNeedNoContractInTheTree(t *testing.T) {
	for name, body := range map[string]string{
		"string literal on a plain string": "package main\nfunc f(r string) bool { return r == \"admin\" }\n",
		"case literal":                     "package main\nfunc f(r string) { switch r { case \"member\": } }\n",
		"a file that does not parse":       "package main\nfunc f( {\n",
	} {
		got := scanInlineRoleChecks(map[string]string{"cmd/meterwised/x.go": body})
		if r := rules(got); !slices.Equal(r, []string{"H11"}) {
			t.Errorf("%s: %v", name, r)
		}
	}
}

func TestLegalRoleUsesStayGreen(t *testing.T) {
	good := map[string]string{
		"internal/contract/ports.go": roleStub,
		// the owners compare roles
		"internal/access/evaluator.go": "package access\nimport \"" + cn + "\"\nfunc f(r contract.Role) bool { return r == contract.RoleAdmin }\n",
		"internal/store/members.go":    "package store\nimport \"" + cn + "\"\nfunc f(r string) bool { return r == string(contract.RoleAdmin) }\n",
		// tests may assert on roles
		"internal/api/x_test.go": "package api\nimport \"" + cn + "\"\nvar _ = contract.RoleAdmin == contract.RoleMember\n",
		// passing, storing, converting a role or calling Valid is not a decision; neither is asking access
		"internal/tenancy/ok.go": roleFile(stdImports, `type inv struct{ Role string }
func g(s S, e string, ids []contract.Role) (inv, bool, error) {
	var role contract.Role = contract.RoleAdmin
	x := contract.Principal{Role: role}
	_ = x
	_ = fmt.Sprintf("role %s", s.Principal.Role)
	_ = strings.ToLower("ADMIN")
	_ = len(ids) + 1
	if !role.Valid() {
		return inv{}, false, nil
	}
	return inv{Role: string(s.Principal.Role)}, mayAssign(s.Principal.Role, role) == nil, nil
}
func mayAssign(a, b contract.Role) error { return nil }`),
		// text that only looks like a check
		"internal/api/prose.go": "package api\n// the old check was p.Role == contract.RoleAdmin\n/* r == \"admin\" */\nvar x = \"p.Role == contract.RoleAdmin\"\nvar y = \"//\"\nvar z = 1\n",
	}
	if got := scanInlineRoleChecks(good); len(got) != 0 {
		t.Fatalf("legal uses flagged: %v", got)
	}
}

// An import cycle (what the H0 and H2 mutations create) must end the scan, not recurse forever.
func TestRoleScanSurvivesAnImportCycle(t *testing.T) {
	files := map[string]string{
		"internal/contract/ports.go": "package contract\nimport _ \"" + module + "/internal/tenancy\"\ntype Role string\n",
		"internal/tenancy/a.go":      "package tenancy\nimport _ \"" + cn + "\"\nfunc f() {}\n",
	}
	done := make(chan struct{})
	go func() { scanInlineRoleChecks(files); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the role scan did not return on an import cycle")
	}
}
