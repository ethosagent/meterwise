package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strings"
)

// httpClientOwners may build an HTTP client: memory (its own model calls, ADR 0033), identity (the Clerk adapter) and
// the commands. Everything else reaches the network only through the proxy (tollgate) or not at all.
var httpClientOwners = []string{"internal/memory/", "internal/identity/", "cmd/"}

// scanHTTPClients applies H12: outside the owners, non-test Go code never names net/http's Client type and never
// calls the package-level helpers that run on http.DefaultClient (Get, Post, PostForm, Head). A request our own key
// signs is the thing this keeps in one place; an unreviewed second client could send it, or provider traffic, elsewhere.
func scanHTTPClients(files map[string]string) []Violation {
	var rels []string
	for rel := range files {
		if path.Ext(rel) == ".go" && !strings.HasSuffix(rel, "_test.go") && !inTestdata(rel) && !httpClientOwner(rel) {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	var out []Violation
	for _, rel := range rels {
		f, err := parser.ParseFile(token.NewFileSet(), rel, files[rel], parser.SkipObjectResolution)
		if err != nil {
			out = append(out, Violation{"H12", fmt.Sprintf("%s does not parse (%v), so its HTTP clients cannot be audited", rel, err)})
			continue
		}
		if what := httpClientUse(f); what != "" {
			out = append(out, Violation{"H12", fmt.Sprintf("%s uses %s; only internal/memory, internal/identity and cmd/** build an http.Client (ADR 0033)", rel, what)})
		}
	}
	return out
}

func httpClientOwner(rel string) bool {
	for _, p := range httpClientOwners {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// httpClientUse names the first http.Client, http.DefaultClient or http.Get-family use in f, or "".
func httpClientUse(f *ast.File) string {
	local := ""
	for _, im := range f.Imports {
		if strings.Trim(im.Path.Value, `"`) != "net/http" {
			continue
		}
		local = "http"
		if im.Name != nil {
			local = im.Name.Name
		}
	}
	if local == "" || local == "_" {
		return ""
	}
	var found string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || found != "" {
			return found == ""
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != local {
			return true
		}
		switch sel.Sel.Name {
		case "Client", "DefaultClient":
			found = "http." + sel.Sel.Name
		case "Get", "Post", "PostForm", "Head":
			found = "http." + sel.Sel.Name + " (the default client)"
		}
		return true
	})
	return found
}
