package contract_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/nonet"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

func TestRegistryIsComplete(t *testing.T) {
	src, err := os.ReadFile("errors.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range contract.Errors() {
		if !strings.Contains(string(src), fmt.Sprintf("%q", string(s.Code))) {
			t.Errorf("code %q has no constant", s.Code)
		}
		if s.Status < 400 || s.Doc == "" {
			t.Errorf("code %q: status %d doc %q", s.Code, s.Status, s.Doc)
		}
	}
	seen := map[contract.Code]bool{}
	for _, s := range contract.Errors() {
		if seen[s.Code] {
			t.Errorf("duplicate code %q", s.Code)
		}
		seen[s.Code] = true
	}
}

func TestErrorStatusAndUnwrapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		code   contract.Code
		status int
	}{
		{"direct", contract.NewError(contract.CodeGone, "retired"), contract.CodeGone, http.StatusGone},
		{"wrapped", fmt.Errorf("lookup: %w", contract.NewError(contract.CodeNotFound, "no")), contract.CodeNotFound, http.StatusNotFound},
		{"foreign error hides internals", errors.New("pq: password=hunter2"), contract.CodeInternal, http.StatusInternalServerError},
		{"unregistered code is 500", contract.NewError("typo", "x"), "typo", http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := contract.AsError(tc.err)
			if e.Code != tc.code || e.Status() != tc.status {
				t.Fatalf("got %s/%d, want %s/%d", e.Code, e.Status(), tc.code, tc.status)
			}
			if strings.Contains(e.Message, "hunter2") {
				t.Fatal("internal detail leaked into the message")
			}
		})
	}
}

func TestErrorString(t *testing.T) {
	if got := contract.NewError(contract.CodeInvalidRequest, "bad").Error(); got != "invalid_request: bad" {
		t.Fatalf("got %q", got)
	}
	if got := contract.NewError(contract.CodeConfigInvalid, "bad", "a", "b").Error(); !strings.Contains(got, "2 detail") {
		t.Fatalf("got %q", got)
	}
	if _, ok := contract.Spec("nope"); ok {
		t.Fatal("unknown code must not resolve")
	}
}

func TestMoneyRoundTrip(t *testing.T) {
	tests := []struct {
		m contract.MicroUSD
		s string
	}{
		{0, "0.000000"}, {1500, "0.001500"}, {2_500_000, "2.500000"}, {-2_500_000, "-2.500000"}, {1, "0.000001"},
	}
	for _, tc := range tests {
		if got := tc.m.Decimal(); got != tc.s {
			t.Errorf("Decimal(%d) = %q, want %q", tc.m, got, tc.s)
		}
		back, err := contract.ParseMicroUSD(tc.s)
		if err != nil || back != tc.m {
			t.Errorf("Parse(%q) = %d, %v", tc.s, back, err)
		}
	}
	short, err := contract.ParseMicroUSD("1.5")
	if err != nil || short != 1_500_000 {
		t.Errorf("Parse(1.5) = %d, %v", short, err)
	}
	for _, bad := range []string{"", "x", "1.1234567", "1e3", ".5", "--1", "1.-2"} {
		if _, err := contract.ParseMicroUSD(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}

func TestMoneyExtremes(t *testing.T) {
	tests := []struct {
		m contract.MicroUSD
		s string
	}{
		{contract.MicroUSD(math.MinInt64), "-9223372036854.775808"},
		{contract.MicroUSD(math.MaxInt64), "9223372036854.775807"},
		{contract.MicroUSD(math.MinInt64 + 1), "-9223372036854.775807"},
	}
	for _, tc := range tests {
		if got := tc.m.Decimal(); got != tc.s {
			t.Errorf("Decimal(%d) = %q, want %q", int64(tc.m), got, tc.s)
		}
		if back, err := contract.ParseMicroUSD(tc.s); err != nil || back != tc.m {
			t.Errorf("Parse(%q) = %d, %v", tc.s, int64(back), err)
		}
	}
	for _, bad := range []string{"9223372036854.775808", "-9223372036854.775809", "9223372036855", "99999999999999999999"} {
		if _, err := contract.ParseMicroUSD(bad); err == nil {
			t.Errorf("Parse(%q) accepted an out-of-range amount", bad)
		}
	}
}

func TestMoneyJSONIsADecimalString(t *testing.T) {
	type row struct {
		Saved contract.MicroUSD `json:"saved"`
	}
	b, err := json.Marshal(row{Saved: 1500})
	if err != nil || string(b) != `{"saved":"0.001500"}` {
		t.Fatalf("got %s, %v", b, err)
	}
	var back row
	if err := json.Unmarshal(b, &back); err != nil || back.Saved != 1500 {
		t.Fatalf("round trip: %+v, %v", back, err)
	}
	for _, bad := range []string{`{"saved":1500}`, `{"saved":"x"}`} {
		if err := json.Unmarshal([]byte(bad), &back); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestRoutesAreDocumentedAndAuthorised(t *testing.T) {
	names := map[string]bool{}
	for _, d := range contract.DTOs() {
		names[d.Name] = true
		if d.Type.Kind() != reflect.Struct {
			t.Errorf("DTO %s is not a struct", d.Name)
		}
	}
	for _, r := range contract.Routes() {
		if !r.Public && r.Permission == "" {
			t.Errorf("%s %s: non-public route without a permission", r.Method, r.Pattern)
		}
		if r.Response == "" && r.Status != http.StatusNoContent {
			t.Errorf("%s %s: empty response DTO is only for a 204", r.Method, r.Pattern)
		}
		if r.Response != "" && !names[r.Response] {
			t.Errorf("%s %s: response DTO %q not registered", r.Method, r.Pattern, r.Response)
		}
		if r.Request != "" && !names[r.Request] {
			t.Errorf("%s %s: request DTO %q not registered", r.Method, r.Pattern, r.Request)
		}
		if r.WorkspaceParam != "" && !strings.Contains(r.Pattern, "{"+r.WorkspaceParam+"}") {
			t.Errorf("%s %s: WorkspaceParam %q is not a path parameter of the pattern", r.Method, r.Pattern, r.WorkspaceParam)
		}
	}
	if _, ok := contract.PermissionFor("GET", "/healthz"); !ok {
		t.Fatal("healthz route missing")
	}
	if _, ok := contract.PermissionFor("POST", "/healthz"); ok {
		t.Fatal("unexpected route")
	}
}

func TestForbiddenCarriesItsReasonAsDetails(t *testing.T) {
	e := contract.Forbidden("nope", "no matching grant")
	b := e.Body()
	if e.Status() != http.StatusForbidden || b.Code != contract.CodeForbidden || b.Details["reason"] != "no matching grant" {
		t.Fatalf("%+v", b)
	}
	if got := contract.NewError(contract.CodeForbidden, "x").Body().Details; len(got) != 0 {
		t.Fatalf("a reasonless error must keep empty details, got %v", got)
	}
	for code, status := range map[contract.Code]int{contract.CodeLastAdmin: 409, contract.CodeAlreadyMember: 409, contract.CodeGroupNameTaken: 409} {
		if s, ok := contract.Spec(code); !ok || s.Status != status {
			t.Errorf("%s: %+v %v", code, s, ok)
		}
	}
}

func TestRoleValid(t *testing.T) {
	for r, want := range map[contract.Role]bool{contract.RoleAdmin: true, contract.RoleMember: true, "": false, "owner": false, "Admin": false} {
		if r.Valid() != want {
			t.Errorf("%q.Valid() = %v", r, !want)
		}
	}
}

// Every mutating route says how it is audited: an Audit spec or an explicit NoAudit. A read says
// neither. Each audit action is declared by exactly one row.
func TestMutatingRoutesDeclareAudit(t *testing.T) {
	seen := map[contract.AuditAction]string{}
	for _, r := range contract.Routes() {
		where := r.Method + " " + r.Pattern
		switch {
		case r.Method == http.MethodGet:
			if r.Audit != nil || r.NoAudit {
				t.Errorf("%s: a read declares no audit", where)
			}
		case r.Audit == nil && !r.NoAudit:
			t.Errorf("%s: mutating route declares neither Audit nor NoAudit", where)
		case r.Audit != nil && r.NoAudit:
			t.Errorf("%s: both Audit and NoAudit", where)
		}
		if r.Audit == nil {
			continue
		}
		a := r.Audit
		if !a.Action.Valid() || a.ResourceType == "" || a.ResourceID == nil {
			t.Errorf("%s: incomplete audit spec %+v", where, a)
		}
		if prev, dup := seen[a.Action]; dup {
			t.Errorf("%s: action %s already declared by %s", where, a.Action, prev)
		}
		seen[a.Action] = where
	}
	for _, a := range contract.AuditActions() {
		if _, ok := seen[a]; !ok {
			t.Errorf("action %s is declared by no route", a)
		}
	}
}

func TestRFC3339IsUTCSecondPrecision(t *testing.T) {
	zone := time.FixedZone("x", 5*3600+1800)
	got := contract.RFC3339(time.Date(2026, 3, 4, 10, 30, 15, 999, zone))
	if got != "2026-03-04T05:00:15Z" {
		t.Fatalf("got %q", got)
	}
}
