package api

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// registeredPairs runs every register* method on a bare server (handlers are method values, never called)
// and returns the (row, handler) type pairs register recorded.
func registeredPairs(t *testing.T) []routePair {
	t.Helper()
	s := &server{mux: http.NewServeMux(), registered: map[string]bool{}}
	s.registerTenancy()
	s.registerAccess()
	s.registerWorkspaceURL()
	s.registerLedger()
	s.registerMonitors()
	s.registerTrim()
	s.registerRouter()
	s.registerMemory()
	if len(s.problems) > 0 {
		t.Fatalf("register problems: %v", s.problems)
	}
	return s.pairs
}

// The handler's Go types must be the DTOs the contract row names, and every audited
// row's extractors must run against the types the handler really returns, not only the row's declared ones
// (contract.TestAuditExtractorsRunAgainstTheRoutesDeclaredTypes covers the declared side).
func TestRegisteredHandlerTypesMatchTheContractRowsAndTheirAuditExtractors(t *testing.T) {
	types := map[string]reflect.Type{}
	for _, d := range contract.DTOs() {
		types[d.Name] = d.Type
	}
	pairs := registeredPairs(t)
	audited := 0
	for _, p := range pairs {
		row, ok := contract.PermissionFor(p.method, p.pattern)
		if !ok {
			t.Fatalf("%s %s has no row", p.method, p.pattern)
		}
		check := func(kind, name string, got reflect.Type) {
			if name == "" {
				if got != reflect.TypeFor[none]() {
					t.Errorf("%s %s: row declares no %s, the handler uses %v", p.method, p.pattern, kind, got)
				}
				return
			}
			want, ok := types[name]
			if !ok {
				t.Errorf("%s %s: %s DTO %q is not in contract.DTOs()", p.method, p.pattern, kind, name)
				return
			}
			if got != want && got != reflect.PointerTo(want) {
				t.Errorf("%s %s: row %s is %v, the handler uses %v", p.method, p.pattern, kind, want, got)
			}
		}
		check("Request", row.Request, p.req)
		check("Response", row.Response, p.resp)
		if row.Audit == nil {
			continue
		}
		audited++
		zero := func(ty reflect.Type) any {
			if ty == reflect.TypeFor[none]() {
				return nil
			}
			return reflect.Zero(ty).Interface()
		}
		in := contract.AuditInput{
			Path:      func(name string) string { return "path-" + name },
			Principal: contract.Principal{UserID: "u", OrgID: "o"},
			Req:       zero(p.req),
			Resp:      zero(p.resp),
		}
		run := func(label string, f func()) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s %s: %s panics on the handler's types: %v", p.method, p.pattern, label, r)
				}
			}()
			f()
		}
		run("ResourceID", func() { _ = row.Audit.ResourceID(in) })
		run("WorkspaceID", func() {
			if row.Audit.WorkspaceID != nil {
				_ = row.Audit.WorkspaceID(in)
			}
		})
		run("Meta", func() {
			if row.Audit.Meta != nil {
				_ = row.Audit.Meta(in)
			}
		})
	}
	if audited == 0 || len(pairs) < 40 {
		t.Fatalf("saw %d pairs, %d audited: the registration helper is not recording", len(pairs), audited)
	}
}
