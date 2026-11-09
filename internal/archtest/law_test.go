package archtest

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

const (
	module   = "github.com/ethosagent/meterwise"
	tollgate = "github.com/ethosagent/tollgate"
)

// Violation is one broken rule.
type Violation struct {
	Rule string
	Msg  string
}

func (v Violation) String() string { return v.Rule + " " + v.Msg }

// pkg is what the law needs to know about one Go package.
type pkg struct {
	Rel         string   // path relative to the module root, "." for the root
	Doc         string   // package doc synopsis; empty = no package comment
	Imports     []string // non-test imports
	TestImports []string // in-package and external test imports
}

// allRules lists every rule ID; seeded_test.go must seed each one.
var allRules = []string{"D1", "P1", "H0", "H1", "H2", "H3", "H4", "H5", "H6", "H7", "H8", "H9", "H10", "H11"}

// rank orders the layers of rule H2: a package may import its own rank or
// lower, never higher. contract and nonet are below everything (rank 0).
var rank = map[string]int{
	"contract": 0, "nonet": 0,
	"config": 1, "store": 1,
	"identity": 2, "tenancy": 2, "access": 2, "wsauth": 2, "audit": 2, "savings": 2,
	"dataplane": 3, "history": 3, "monitors": 3, "ledger": 3,
	"api": 4,
}

const cmdRank = 5

// clerkmockPkg is the Clerk twin. It signs tokens with a key anyone can read,
// so production code must never link it (H10).
const clerkmockPkg = module + "/internal/identity/clerkmock"

// clerkmockUsers are the only non-test packages that may import the twin: the
// standalone issuer for local end-to-end runs and the fixture replay tool.
// Neither is built into the production image (the Dockerfile builds meterwised only).
var clerkmockUsers = []string{"cmd/clerkmock-serve", "cmd/clerk-replay"}

// pure are the hot-path packages of rule H3.
var pure = []string{"history", "monitors", "savings"}

// pureBanned are stdlib prefixes pure packages may not import (H3).
var pureBanned = []string{"net", "database/sql", "os/exec"}

// thirdParty is rule H6's allowlist: module prefix -> layers that may import it.
// Adding an entry needs an ADR (docs/decisions/0007). Layer "*" means any.
var thirdParty = map[string][]string{
	tollgate:                        {"*"},
	"github.com/jackc/pgx":          {"store"},
	"github.com/pressly/goose":      {"store"},
	"github.com/go-chi/chi":         {"api"},
	"github.com/clerk/clerk-sdk-go": {"identity"},
	"github.com/svix/svix-webhooks": {"identity"},
	"go.opentelemetry.io/otel":      {"dataplane", "api", "cmd"},
}

// layerOf classifies a package by its path relative to the module root.
func layerOf(rel string) string {
	parts := strings.Split(rel, "/")
	switch {
	case parts[0] == "cmd":
		return "cmd"
	case parts[0] == "internal" && len(parts) > 1:
		if parts[1] == "archtest" {
			return "archtest"
		}
		if _, ok := rank[parts[1]]; ok {
			return parts[1]
		}
	}
	return ""
}

func rankOf(layer string) int {
	if layer == "cmd" {
		return cmdRank
	}
	return rank[layer]
}

func isStd(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")
	return !strings.Contains(first, ".")
}

func underAny(imp string, prefixes ...string) bool {
	for _, p := range prefixes {
		if imp == p || strings.HasPrefix(imp, p+"/") {
			return true
		}
	}
	return false
}

func isOurs(imp string) bool { return underAny(imp, module) }

// internalLayer returns the layer of one of our internal packages, or "".
func internalLayer(imp string) string {
	rel := strings.TrimPrefix(imp, module+"/")
	if !strings.HasPrefix(rel, "internal/") {
		return ""
	}
	return layerOf(rel)
}

// analyze applies the package-level rules (D1 P1 H0 H1 H2 H3 H6).
func analyze(pkgs []pkg) []Violation {
	var out []Violation
	for _, p := range pkgs {
		l := layerOf(p.Rel)
		if l == "" {
			out = append(out, Violation{"D1", p.Rel + ": package is in no layer; add it to ARCHITECTURE.md §3 and rank in internal/archtest/law_test.go"})
			continue
		}
		if p.Doc == "" {
			out = append(out, Violation{"P1", p.Rel + ": package has no doc comment; add doc.go saying what it does and what it may import"})
		}
		for _, imp := range p.Imports {
			out = append(out, checkImport(l, p.Rel, imp, false)...)
		}
		for _, imp := range p.TestImports {
			out = append(out, checkImport(l, p.Rel, imp, true)...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func checkImport(l, rel, imp string, inTest bool) []Violation {
	var out []Violation
	add := func(rule, why string) {
		out = append(out, Violation{rule, fmt.Sprintf("%s imports %s: %s", rel, imp, why)})
	}

	if underAny(imp, tollgate) && imp != tollgate {
		switch {
		case underAny(imp, tollgate+"/ext"):
		case underAny(imp, tollgate+"/testkit"):
			if !inTest {
				add("H1", "tollgate/testkit is for tests only")
			}
		default:
			add("H1", "only tollgate's root, ext/** and (tests only) testkit are public; internal/, cmd/ and the rest are not")
		}
	}
	if l == "contract" && !inTest && !isStd(imp) && imp != tollgate {
		add("H0", "internal/contract imports the standard library and the tollgate root only")
	}
	if underAny(imp, clerkmockPkg) && !inTest && rel != "internal/identity/clerkmock" && !slices.Contains(clerkmockUsers, rel) {
		add("H10", "the Clerk twin signs tokens with a published key; only tests and cmd/clerkmock-serve (and cmd/clerk-replay) may import it")
	}
	if isOurs(imp) {
		checkLayering(l, imp, add)
	}
	if slices.Contains(pure, l) && !inTest {
		checkPure(imp, add)
	}
	if !isStd(imp) && !isOurs(imp) {
		checkThirdParty(l, imp, add)
	}
	return out
}

func checkLayering(l, imp string, add func(rule, why string)) {
	il := internalLayer(imp)
	switch {
	case strings.HasPrefix(strings.TrimPrefix(imp, module+"/"), "cmd"):
		add("H2", "nothing imports a command")
	case il == "":
		add("D1", "imported package is in no layer")
	case il == "archtest":
		add("H2", "nothing imports archtest")
	case l == "archtest":
		add("H2", "archtest depends on the standard library only")
	case rankOf(il) > rankOf(l):
		add("H2", fmt.Sprintf("%s (rank %d) may not import %s (rank %d): lower layers never import higher ones", l, rankOf(l), il, rankOf(il)))
	}
}

func checkPure(imp string, add func(rule, why string)) {
	switch {
	case isStd(imp):
		if underAny(imp, pureBanned...) {
			add("H3", "hot-path packages (history, monitors, savings) do no network, database or process I/O")
		}
	case imp == tollgate, imp == module+"/internal/contract":
	default:
		add("H3", "hot-path packages import only the standard library, tollgate's root and internal/contract")
	}
}

func checkThirdParty(l, imp string, add func(rule, why string)) {
	for prefix, layers := range thirdParty {
		if underAny(imp, prefix) && (slices.Contains(layers, "*") || slices.Contains(layers, l)) {
			return
		}
	}
	add("H6", fmt.Sprintf("third-party import not on the allowlist for layer %q (needs an ADR and an entry in law_test.go)", l))
}
