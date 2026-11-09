package archtest

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"sort"
	"strings"
)

// roleOwners may compare roles: contract defines them, access decides with them (the evaluator
// and the escalation guard), store keeps the last-admin invariant in its transactions.
var roleOwners = []string{"internal/contract", "internal/access", "internal/store"}

const roleType = module + "/internal/contract"

// roleHelpers are the standard-library packages whose functions compare, search or match their
// arguments. Handing them a role (or anything derived from one) is an inline role check.
var roleHelpers = map[string]bool{
	"strings": true, "bytes": true, "slices": true, "maps": true, "cmp": true,
	"reflect": true, "regexp": true, "sort": true, "unicode": true, "unicode/utf8": true,
}

// scanInlineRoleChecks applies H11: outside the role owners, non-test Go code never decides
// access by looking at a role. Every route names a permission in the contract table and the one
// gateway middleware asks internal/access; handlers and services call access (IsAdmin,
// MayAssignRole) or take the decision as an argument.
//
// The scan is on type information, not text, so the spelling does not matter: an import alias,
// a dot import, a local constant or variable, a conversion, parentheses, a method that returns a
// role, a string literal in any quoting. It flags a comparison, a switch tag or case, a map
// lookup keyed by a role, or a comparison helper from the standard library, whenever the value
// is a contract.Role or was derived from one (string(r), len(r), fmt.Sprint(r), a local copy).
// Any comparison with the constant "admin" or "member" is flagged too. Passing a role on, storing
// it, converting it for a DTO and calling its Valid method are not decisions.
func scanInlineRoleChecks(files map[string]string) []Violation {
	ld := &roleLoader{files: files, pkgs: map[string]*types.Package{}, busy: map[string]bool{}, sum: map[string]paramSummary{}}
	var dirs []string
	byDir := map[string][]string{}
	for rel := range files {
		if path.Ext(rel) != ".go" || strings.HasSuffix(rel, "_test.go") || inTestdata(rel) ||
			strings.HasPrefix(rel, "internal/archtest/") || underDirs(rel, roleOwners) {
			continue
		}
		d := path.Dir(rel)
		if byDir[d] == nil {
			dirs = append(dirs, d)
		}
		byDir[d] = append(byDir[d], rel)
	}
	sort.Strings(dirs)
	var out []Violation
	for _, d := range dirs {
		out = append(out, ld.scanDir(d, byDir[d])...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Msg < out[j].Msg })
	return out
}

// roleLoader type-checks packages of the scanned tree from the files map, which is the whole
// repository: module-local imports resolve from it, everything else (the standard library, third
// parties) is left unresolved. Checker errors are tolerated on purpose: the scan needs the types
// that did resolve, and a package that does not compile is stopped by the vet gate.
type roleLoader struct {
	files map[string]string
	pkgs  map[string]*types.Package
	busy  map[string]bool // packages being checked: an import cycle (H2 and H0 break it on purpose) ends here
	sum   map[string]paramSummary
}

func (l *roleLoader) Import(ip string) (*types.Package, error) {
	rel, ok := strings.CutPrefix(ip, module+"/")
	if !ok {
		return nil, errors.New("not in the module: " + ip)
	}
	if p, ok := l.pkgs[ip]; ok {
		return p, nil
	}
	if l.busy[ip] {
		return nil, errors.New("import cycle through " + ip)
	}
	l.busy[ip] = true
	defer delete(l.busy, ip)
	var names []string
	for f := range l.files {
		if path.Dir(f) == rel && path.Ext(f) == ".go" && !strings.HasSuffix(f, "_test.go") {
			names = append(names, f)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("no Go files in " + rel)
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	var parsed []*ast.File
	for _, n := range names {
		if f, err := parser.ParseFile(fset, n, l.files[n], parser.SkipObjectResolution); err == nil {
			parsed = append(parsed, f)
		}
	}
	conf := types.Config{Importer: l, Error: func(error) {}, IgnoreFuncBodies: true}
	p, _ := conf.Check(ip, fset, parsed, nil)
	l.pkgs[ip] = p
	l.sum[ip] = compareSummaries(parsed)
	return p, nil
}

func (l *roleLoader) scanDir(dir string, rels []string) []Violation {
	sort.Strings(rels)
	fset := token.NewFileSet()
	var parsed []*ast.File
	var out []Violation
	for _, rel := range rels {
		f, err := parser.ParseFile(fset, rel, l.files[rel], parser.SkipObjectResolution)
		if err != nil {
			out = append(out, Violation{"H11", fmt.Sprintf("%s does not parse (%v), so its role checks cannot be audited", rel, err)})
			continue
		}
		parsed = append(parsed, f)
	}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Uses:  map[*ast.Ident]types.Object{},
		Defs:  map[*ast.Ident]types.Object{},
	}
	conf := types.Config{Importer: l, Error: func(error) {}}
	_, _ = conf.Check(module+"/"+dir, fset, parsed, info)
	l.sum[module+"/"+dir] = compareSummaries(parsed)
	for _, f := range parsed {
		w := &roleWalker{info: info, taint: map[types.Object]bool{}, lits: map[types.Object]*ast.FuncLit{}, sums: func(p string) paramSummary { return l.sum[p] }}
		w.run(f)
		if len(w.found) == 0 {
			continue
		}
		rel := fset.Position(f.Pos()).Filename
		sort.Slice(w.found, func(i, j int) bool { return w.found[i].pos < w.found[j].pos })
		first, more := w.found[0], ""
		if n := len(w.found) - 1; n > 0 {
			more = fmt.Sprintf(" and %d more", n)
		}
		out = append(out, Violation{"H11", fmt.Sprintf("%s checks a role inline (%s, line %d%s); name a permission in the contract route table and let the gateway and internal/access decide", rel, first.what, fset.Position(first.pos).Line, more)})
	}
	return out
}

type roleFinding struct {
	pos  token.Pos
	what string
}

type roleWalker struct {
	info  *types.Info
	taint map[types.Object]bool // locals, fields and channels that received a role-derived value
	lits  map[types.Object]*ast.FuncLit
	sums  func(pkg string) paramSummary
	found []roleFinding
}

func (w *roleWalker) run(f *ast.File) {
	// Passes so a value copied through a local, a field, a channel or a closure is tainted before
	// its comparison is seen.
	for range flowPasses {
		ast.Inspect(f, func(n ast.Node) bool { w.flow(n); return true })
	}
	ast.Inspect(f, w.visit)
}

func (w *roleWalker) add(n ast.Node, what string) {
	w.found = append(w.found, roleFinding{n.Pos(), what})
}

func (w *roleWalker) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.BinaryExpr:
		switch n.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			if w.derived(n.X) || w.derived(n.Y) {
				w.add(n, "a comparison of a role")
			} else if w.roleWord(n.X) || w.roleWord(n.Y) {
				w.add(n, `a comparison with "admin" or "member"`)
			} else if holdsRole(w.info.TypeOf(n.X), map[types.Type]bool{}) || holdsRole(w.info.TypeOf(n.Y), map[types.Type]bool{}) {
				w.add(n, "a comparison of a struct holding a role")
			}
		}
	case *ast.SwitchStmt:
		if n.Tag != nil && w.derived(n.Tag) {
			w.add(n, "a switch on a role")
		}
	case *ast.CaseClause:
		for _, e := range n.List {
			if w.derived(e) || w.roleWord(e) {
				w.add(e, "a case on a role")
			}
		}
	case *ast.IndexExpr:
		if t := w.info.TypeOf(n.X); t != nil {
			if _, isMap := t.Underlying().(*types.Map); isMap && w.derived(n.Index) {
				w.add(n, "a map lookup by role")
			}
		}
	case *ast.CallExpr:
		w.visitCall(n)
	}
	return true
}

func (w *roleWalker) visitCall(c *ast.CallExpr) {
	w.calleeFlow(c)
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}
	pn, ok := w.info.Uses[id].(*types.PkgName)
	if !ok {
		return
	}
	switch p := pn.Imported().Path(); {
	case p == roleType && sel.Sel.Name == "Allows":
		w.add(c, "contract.Allows")
	case roleHelpers[p]:
		for _, a := range c.Args {
			if w.derived(a) || w.roleSlice(a) {
				w.add(c, p+"."+sel.Sel.Name+" on a role")
				return
			}
		}
	}
}

// isRole reports whether t is contract.Role (or a pointer to it).
func isRole(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Name() == "Role" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == roleType
}

func (w *roleWalker) roleSlice(e ast.Expr) bool {
	t := w.info.TypeOf(e)
	if t == nil {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return isRole(u.Elem())
	case *types.Array:
		return isRole(u.Elem())
	}
	return false
}

// roleWord reports whether e is a constant equal to "admin" or "member", however it is spelled
// (interpreted or raw literal, escapes, concatenation, a named constant).
func (w *roleWalker) roleWord(e ast.Expr) bool {
	tv, ok := w.info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	s := constant.StringVal(tv.Value)
	return s == "admin" || s == "member"
}

// derived reports whether e is a contract.Role, or computed from one: a conversion, a builtin or
// a helper from the standard library applied to one, an index or slice of one, or a local that was
// assigned from one.
func (w *roleWalker) derived(e ast.Expr) bool {
	if t := w.info.TypeOf(e); t != nil && isRole(t) {
		return true
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return w.derived(e.X)
	case *ast.Ident:
		o := w.info.Uses[e]
		return o != nil && w.taint[o]
	case *ast.UnaryExpr:
		return w.derived(e.X)
	case *ast.StarExpr:
		return w.derived(e.X)
	case *ast.IndexExpr:
		return w.derived(e.X)
	case *ast.SelectorExpr:
		return w.roleField(e) || w.taint[w.info.Uses[e.Sel]]
	case *ast.CompositeLit:
		return w.derivedElems(e)
	case *ast.SliceExpr:
		return w.derived(e.X)
	case *ast.BinaryExpr:
		return e.Op == token.ADD && (w.derived(e.X) || w.derived(e.Y)) // string(r) + "x"
	case *ast.CallExpr:
		return w.derivedCall(e)
	}
	return false
}

func (w *roleWalker) derivedCall(c *ast.CallExpr) bool {
	conv := false
	if tv, ok := w.info.Types[c.Fun]; ok && tv.IsType() {
		conv = true
	}
	if id, ok := c.Fun.(*ast.Ident); ok {
		if _, builtin := w.info.Uses[id].(*types.Builtin); builtin {
			conv = true
		}
	}
	if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			if pn, ok := w.info.Uses[id].(*types.PkgName); ok && !strings.HasPrefix(pn.Imported().Path(), module+"/") {
				conv = true // fmt.Sprint(role), strings.ToLower(string(role)) keep the role's value
			}
		}
	}
	if !conv {
		return false
	}
	for _, a := range c.Args {
		if w.derived(a) {
			return true
		}
	}
	return false
}
