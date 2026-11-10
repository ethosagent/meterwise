package contract_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// auditInputFor builds the input the api wrapper hands a route's extractors after a success: the
// route's declared Request and Response DTO as values (zero ones), nil where the row declares none.
func auditInputFor(t *testing.T, r contract.Route) contract.AuditInput {
	t.Helper()
	types := map[string]reflect.Type{}
	for _, d := range contract.DTOs() {
		types[d.Name] = d.Type
	}
	zero := func(name string) any {
		if name == "" {
			return nil
		}
		ty, ok := types[name]
		if !ok {
			t.Fatalf("%s %s: DTO %q is not in DTOs()", r.Method, r.Pattern, name)
		}
		return reflect.Zero(ty).Interface()
	}
	return contract.AuditInput{
		Path:      func(name string) string { return "path-" + name },
		Principal: contract.Principal{UserID: "u", OrgID: "o"},
		Req:       zero(r.Request),
		Resp:      zero(r.Response),
	}
}

// safely runs f and turns a panic (a wrong type assertion in an extractor) into an error.
func safely(f func()) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	f()
	return nil
}

// Every audited route's extractors run against the DTO types the row itself declares, so a wrong
// type assertion fails this test instead of panicking in a handler after the change committed.
func TestAuditExtractorsRunAgainstTheRoutesDeclaredTypes(t *testing.T) {
	audited := 0
	for _, r := range contract.Routes() {
		if r.Audit == nil {
			continue
		}
		audited++
		t.Run(r.Method+" "+r.Pattern, func(t *testing.T) {
			sp, in := r.Audit, auditInputFor(t, r)
			if sp.Action == "" || sp.ResourceType == "" {
				t.Fatalf("spec %+v lacks an action or a resource type", sp)
			}
			if sp.ResourceID == nil {
				t.Fatal("ResourceID is nil: the api wrapper calls it on every audited success")
			}
			if err := safely(func() { _ = sp.ResourceID(in) }); err != nil {
				t.Errorf("ResourceID: %v", err)
			}
			if sp.WorkspaceID != nil {
				if err := safely(func() { _ = sp.WorkspaceID(in) }); err != nil {
					t.Errorf("WorkspaceID: %v", err)
				}
			}
			if sp.Meta != nil {
				if err := safely(func() { _ = sp.Meta(in) }); err != nil {
					t.Errorf("Meta: %v", err)
				}
			}
		})
	}
	if audited == 0 {
		t.Fatal("no audited route found")
	}
}

func TestAuditExtractorCheckCatchesAWrongTypeAssertion(t *testing.T) {
	wrong := func(in contract.AuditInput) string { return in.Resp.(contract.WorkspaceView).ID }
	r := contract.Route{Method: "POST", Pattern: "/x", Request: "AllowedCIDRs", Response: "AllowedCIDRs"}
	if err := safely(func() { _ = wrong(auditInputFor(t, r)) }); err == nil {
		t.Fatal("a wrong assertion must surface as an error")
	}
}
