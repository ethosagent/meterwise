package archtest

import (
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"strconv"
	"strings"
)

// The flow half of H11: where a role goes after it is read. The walker taints locals, struct
// fields and channels that receive a role-derived value, the parameters of a closure it is passed
// to, and flags a call into a repo function whose parameter is compared inside that function.
// Everything here is bounded: a fixed number of passes, a visited set on every type walk, and one
// summary per package.

const flowPasses = 3

// paramSummary maps a function key ("Name" or "Recv.Name") to the parameter indexes the function
// compares, switches on, or hands to a comparison helper (directly or through another function of
// its package).
type paramSummary map[string]map[int]bool

func funcKey(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	t := d.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
			continue
		case *ast.ParenExpr:
			t = x.X
			continue
		case *ast.IndexExpr:
			t = x.X
			continue
		case *ast.IndexListExpr:
			t = x.X
			continue
		case *ast.Ident:
			return x.Name + "." + d.Name.Name
		}
		return d.Name.Name
	}
}

func paramNames(ft *ast.FuncType) []string {
	var out []string
	for _, f := range ft.Params.List {
		if len(f.Names) == 0 {
			out = append(out, "")
		}
		for _, n := range f.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

func importNames(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, im := range f.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			continue
		}
		n := path.Base(p)
		if im.Name != nil {
			n = im.Name.Name
		}
		m[n] = p
	}
	return m
}

// compareSummaries computes the paramSummary of a package from its syntax alone, so it works on
// imported packages whose function bodies were not type-checked.
func compareSummaries(files []*ast.File) paramSummary {
	type edge struct {
		callee string
		arg    int
		from   string
		param  int
	}
	sum := paramSummary{}
	var edges []edge
	for _, f := range files {
		imps := importNames(f)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			key, names := funcKey(fd), paramNames(fd.Type)
			idx := func(e ast.Expr) []int {
				var hit []int
				ast.Inspect(e, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok {
						for i, nm := range names {
							if nm != "" && nm == id.Name {
								hit = append(hit, i)
							}
						}
					}
					return true
				})
				return hit
			}
			mark := func(e ast.Expr) {
				for _, i := range idx(e) {
					if sum[key] == nil {
						sum[key] = map[int]bool{}
					}
					sum[key][i] = true
				}
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BinaryExpr:
					switch n.Op {
					case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
						mark(n.X)
						mark(n.Y)
					}
				case *ast.SwitchStmt:
					if n.Tag != nil {
						mark(n.Tag)
					}
				case *ast.CaseClause:
					for _, e := range n.List {
						mark(e)
					}
				case *ast.CallExpr:
					switch fn := n.Fun.(type) {
					case *ast.SelectorExpr:
						if x, ok := fn.X.(*ast.Ident); ok && roleHelpers[imps[x.Name]] {
							for _, a := range n.Args {
								mark(a)
							}
						}
					case *ast.Ident:
						for ai, a := range n.Args {
							for _, pi := range idx(a) {
								edges = append(edges, edge{fn.Name, ai, key, pi})
							}
						}
					}
				}
				return true
			})
		}
	}
	for range 5 { // forwarding through package-local helpers, to a fixed point or five rounds
		changed := false
		for _, e := range edges {
			if sum[e.callee][e.arg] && !sum[e.from][e.param] {
				if sum[e.from] == nil {
					sum[e.from] = map[int]bool{}
				}
				sum[e.from][e.param] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return sum
}

// target resolves the object a value is stored into: a local, a struct field, or the container
// of an index, dereference or parenthesis.
func (w *roleWalker) target(e ast.Expr) types.Object {
	switch e := e.(type) {
	case *ast.Ident:
		if o := w.info.Defs[e]; o != nil {
			return o
		}
		return w.info.Uses[e]
	case *ast.SelectorExpr:
		return w.info.Uses[e.Sel]
	case *ast.IndexExpr:
		return w.target(e.X)
	case *ast.StarExpr:
		return w.target(e.X)
	case *ast.ParenExpr:
		return w.target(e.X)
	}
	return nil
}

func (w *roleWalker) taintTarget(e ast.Expr) {
	if o := w.target(e); o != nil {
		w.taint[o] = true
	}
}

// flow applies one statement or expression to the taint sets.
func (w *roleWalker) flow(n ast.Node) {
	switch n := n.(type) {
	case *ast.AssignStmt:
		if len(n.Lhs) == len(n.Rhs) {
			for i := range n.Lhs {
				w.bind(n.Lhs[i], n.Rhs[i])
			}
		} else if len(n.Rhs) == 1 && w.derived(n.Rhs[0]) {
			for _, l := range n.Lhs {
				w.taintTarget(l)
			}
		}
	case *ast.ValueSpec:
		if len(n.Names) == len(n.Values) {
			for i := range n.Names {
				w.bind(n.Names[i], n.Values[i])
			}
		}
	case *ast.SendStmt:
		if w.derived(n.Value) {
			w.taintTarget(n.Chan)
		}
	case *ast.RangeStmt:
		if w.derived(n.X) {
			for _, e := range []ast.Expr{n.Key, n.Value} {
				if e != nil {
					w.taintTarget(e)
				}
			}
		}
	case *ast.CompositeLit:
		w.flowLiteral(n)
	case *ast.CallExpr:
		w.flowClosure(n)
	}
}

func (w *roleWalker) bind(lhs, rhs ast.Expr) {
	if fl, ok := rhs.(*ast.FuncLit); ok {
		if o := w.target(lhs); o != nil {
			w.lits[o] = fl
		}
	}
	if w.derived(rhs) {
		w.taintTarget(lhs)
	}
}

// flowLiteral taints the struct fields a role-derived value is stored into by a literal.
func (w *roleWalker) flowLiteral(c *ast.CompositeLit) {
	t := w.info.TypeOf(c)
	if t == nil {
		return
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return
	}
	for i, el := range c.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if k, ok := kv.Key.(*ast.Ident); ok && w.derived(kv.Value) {
				if o := w.info.Uses[k]; o != nil {
					w.taint[o] = true
				}
			}
		} else if i < st.NumFields() && w.derived(el) {
			w.taint[st.Field(i)] = true
		}
	}
}

// flowClosure carries a role-derived argument into the parameter of a function literal that is
// called at once, or through a local it was bound to.
func (w *roleWalker) flowClosure(c *ast.CallExpr) {
	var lit *ast.FuncLit
	switch f := c.Fun.(type) {
	case *ast.FuncLit:
		lit = f
	case *ast.Ident:
		lit = w.lits[w.info.Uses[f]]
	}
	if lit == nil {
		return
	}
	var params []*ast.Ident
	for _, f := range lit.Type.Params.List {
		params = append(params, f.Names...)
	}
	for i, a := range c.Args {
		if i < len(params) && w.derived(a) {
			w.taintTarget(params[i])
		}
	}
}

// holdsRole reports whether comparing values of type t compares a contract.Role: a struct or
// array with a role field, at any depth. seen ends a recursive type.
func holdsRole(t types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	switch u := t.Underlying().(type) {
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if ft := u.Field(i).Type(); isRole(ft) || holdsRole(ft, seen) {
				return true
			}
		}
	case *types.Array:
		return isRole(u.Elem()) || holdsRole(u.Elem(), seen)
	}
	return false
}

// roleField is a plain-string field or method result named Role: a role that lost its type.
func (w *roleWalker) roleField(e *ast.SelectorExpr) bool {
	if e.Sel.Name != "Role" {
		return false
	}
	b, ok := w.info.TypeOf(e).(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// calleeFlow flags a call into a repo function that compares the parameter a role-derived (or
// "admin"/"member") argument lands in. The role owners are the sanctioned callees.
func (w *roleWalker) calleeFlow(c *ast.CallExpr) {
	f := c.Fun
	for {
		switch x := f.(type) {
		case *ast.ParenExpr:
			f = x.X
			continue
		case *ast.IndexExpr:
			f = x.X
			continue
		case *ast.IndexListExpr:
			f = x.X
			continue
		}
		break
	}
	var id *ast.Ident
	switch x := f.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	}
	if id == nil {
		return
	}
	fn, ok := w.info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return
	}
	rel, ok := strings.CutPrefix(fn.Pkg().Path(), module+"/")
	if !ok || underDirs(rel+"/x.go", roleOwners) {
		return
	}
	fn = fn.Origin()
	key := fn.Name()
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		rt := sig.Recv().Type()
		if p, ok := rt.(*types.Pointer); ok {
			rt = p.Elem()
		}
		if n, ok := rt.(*types.Named); ok {
			key = n.Obj().Name() + "." + key
		}
	}
	params := w.sums(fn.Pkg().Path())[key]
	for i, a := range c.Args {
		if params[i] && (w.derived(a) || w.roleWord(a)) {
			w.add(c, "a call to "+fn.Name()+", which compares that argument")
			return
		}
	}
}

// derivedElems: a slice, array or map literal that holds a role-derived element is role-derived.
func (w *roleWalker) derivedElems(c *ast.CompositeLit) bool {
	if t := w.info.TypeOf(c); t != nil {
		if _, isStruct := t.Underlying().(*types.Struct); isStruct {
			return false // structs are tracked field by field
		}
	}
	for _, el := range c.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			el = kv.Value
		}
		if w.derived(el) {
			return true
		}
	}
	return false
}
