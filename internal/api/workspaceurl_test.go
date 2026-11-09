package api_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
)

func idOf(url string) string { return url[strings.LastIndex(url, "/")+1:] }

func TestSetupRendersTheURLAndSnippets(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	pub, id := s.defaultWorkspace(t, admin.tok)
	su := decodeAs[contract.Setup](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+id+"/setup", "", 200))
	if su.ProxyURL != "https://proxy.test/w/"+pub || !strings.Contains(su.Snippets.OpencodeJSON, su.ProxyURL) || !strings.Contains(su.Snippets.OpenaiEnv, su.ProxyURL) {
		t.Fatalf("setup = %+v", su)
	}
	if su.Status.FirstCallAt != nil || su.Status.LastCallAt != nil || su.Status.CallsToday != 0 {
		t.Fatalf("status = %+v", su.Status)
	}
	code, b := s.call(t, "GET", "/api/workspaces/00000000-0000-0000-0000-00000000dead/setup", admin.tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)
}

func TestResetURLKeepsTheOldIDInGraceThenGoneAndIsAudited(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	old, id := s.defaultWorkspace(t, admin.tok)
	before := time.Now()
	res := decodeAs[contract.ResetURLResponse](t, s.must(t, admin.tok, "POST", "/api/workspaces/"+id+"/url/reset", `{"graceMinutes":30}`, 200))
	fresh := idOf(res.ProxyURL)
	until, err := time.Parse(time.RFC3339, res.PreviousGraceUntil)
	if err != nil || fresh == old || len(fresh) != 22 || until.Before(before.Add(29*time.Minute)) || until.After(time.Now().Add(31*time.Minute)) {
		t.Fatalf("reset = %+v (%v)", res, err)
	}
	dir := s.svc.Directory()
	if w, err := dir.ByPublicID(context.Background(), old); err != nil || !w.Retired || w.ID != id {
		t.Fatalf("old id in grace: %+v %v", w, err)
	}
	if w, err := dir.ByPublicID(context.Background(), fresh); err != nil || w.Retired {
		t.Fatalf("new id: %+v %v", w, err)
	}
	if got, _ := s.defaultWorkspace(t, admin.tok); got != fresh {
		t.Fatalf("the list shows %q, want the new id", got)
	}

	s.must(t, admin.tok, "POST", "/api/workspaces/"+id+"/url/retire-previous", "", 204)
	_, err = dir.ByPublicID(context.Background(), old)
	if contract.AsError(err).Code != contract.CodeWorkspaceURLReset {
		t.Fatalf("after retire-previous: %v", err)
	}

	list := decodeAs[contract.AuditList](t, s.must(t, admin.tok, "GET", "/api/audit-log?workspaceId="+id, "", 200))
	if len(list.Items) != 2 || list.Items[0].Action != contract.AuditWorkspaceURLRetire || list.Items[1].Action != contract.AuditWorkspaceURLReset {
		t.Fatalf("audit = %+v", list.Items)
	}
	e := list.Items[1]
	if e.Actor == nil || e.Actor.UserID != admin.id || e.Actor.Email != admin.email || e.IP == nil || e.ResourceID == nil || *e.ResourceID != id || e.Metadata["graceMinutes"] != float64(30) {
		t.Fatalf("entry = %+v", e)
	}
	raw := s.must(t, admin.tok, "GET", "/api/audit-log", "", 200)
	if strings.Contains(string(raw), old) || strings.Contains(string(raw), fresh) || strings.Contains(s.logs.String(), old) || strings.Contains(s.logs.String(), fresh) {
		t.Fatal("a public workspace id leaked into the audit log or the server log")
	}
}

func TestResetURLValidatesTheGraceAndTheDefaultIsTenMinutes(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, id := s.defaultWorkspace(t, admin.tok)
	for _, body := range []string{`{"graceMinutes":-1}`, `{"graceMinutes":1441}`, `{"graceMinutes":"x"}`, `{"other":1}`} {
		code, b := s.call(t, "POST", "/api/workspaces/"+id+"/url/reset", admin.tok, body)
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
	}
	if n := s.count(t, `SELECT count(*) FROM audit_log`); n != 0 {
		t.Fatalf("a refused reset wrote %d audit rows", n)
	}
	t0 := time.Now()
	res := decodeAs[contract.ResetURLResponse](t, s.must(t, admin.tok, "POST", "/api/workspaces/"+id+"/url/reset", `{}`, 200))
	until, _ := time.Parse(time.RFC3339, res.PreviousGraceUntil)
	if d := until.Sub(t0); d < 9*time.Minute || d > 11*time.Minute {
		t.Fatalf("default grace = %v", d)
	}
	s.must(t, admin.tok, "POST", "/api/workspaces/"+id+"/url/reset", `{"graceMinutes":0}`, 200)
	code, b := s.call(t, "POST", "/api/workspaces/00000000-0000-0000-0000-00000000dead/url/reset", admin.tok, `{}`)
	wantErr(t, code, b, 404, contract.CodeNotFound)
}

func TestAllowedCIDRsNormaliseValidateAndAppearOnTheWorkspace(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, id := s.defaultWorkspace(t, admin.tok)
	got := decodeAs[contract.AllowedCIDRs](t, s.must(t, admin.tok, "PUT", "/api/workspaces/"+id+"/url/allowed-cidrs", `{"cidrs":["10.1.2.3/8","203.0.113.7"]}`, 200))
	if strings.Join(got.CIDRs, ",") != "10.0.0.0/8,203.0.113.7/32" {
		t.Fatalf("cidrs = %v", got.CIDRs)
	}
	for _, body := range []string{`{"cidrs":["nope"]}`, `{"cidrs":["1.2.3.4/33"]}`, `{"cidrs":["` + strings.Repeat(`1.1.1.1","`, 32) + `1.1.1.1"]}`} {
		code, b := s.call(t, "PUT", "/api/workspaces/"+id+"/url/allowed-cidrs", admin.tok, body)
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
	}
	list := decodeAs[contract.WorkspaceList](t, s.must(t, admin.tok, "GET", "/api/workspaces", "", 200))
	if c := list.Items[0].AllowedClientCidrs; c == nil || len(*c) != 2 {
		t.Fatalf("workspace view cidrs = %v", c)
	}
	cleared := decodeAs[contract.AllowedCIDRs](t, s.must(t, admin.tok, "PUT", "/api/workspaces/"+id+"/url/allowed-cidrs", `{"cidrs":[]}`, 200))
	if cleared.CIDRs == nil || len(cleared.CIDRs) != 0 {
		t.Fatalf("cleared = %#v", cleared)
	}
	if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action = 'workspace.allowed_cidrs_update'`); n != 2 {
		t.Fatalf("audit rows = %d", n)
	}
}

func TestTheURLIsNullWithoutWorkspaceURLRead(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	_, defID := s.defaultWorkspace(t, admin.tok)

	visible := decodeAs[contract.WorkspaceList](t, s.must(t, m.tok, "GET", "/api/workspaces", "", 200))
	if len(visible.Items) == 0 || visible.Items[0].PublicID == nil {
		t.Fatalf("members read the URL by default: %+v", visible.Items)
	}
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", `{"grants":[{"resourceType":"workspace","action":"read","workspaceId":null}]}`, 200)
	hidden := decodeAs[contract.WorkspaceList](t, s.must(t, m.tok, "GET", "/api/workspaces", "", 200))
	w := hidden.Items[0]
	if w.PublicID != nil || w.ProxyURL != nil || w.AllowedClientCidrs != nil || w.ID != defID {
		t.Fatalf("the URL leaked to a caller without workspace_url:read: %+v", w)
	}
	if strings.Contains(string(s.must(t, m.tok, "GET", "/api/workspaces", "", 200)), `"publicId":"`) {
		t.Fatal("publicId present in the body")
	}
	code, b := s.call(t, "GET", "/api/workspaces/"+defID+"/setup", m.tok, "")
	forbidden(t, code, b, "no matching grant")
	code, b = s.call(t, "POST", "/api/workspaces/"+defID+"/url/reset", m.tok, `{}`)
	forbidden(t, code, b, "no matching grant")
	// the admin still sees it, and so does a create/patch response for the admin
	if v := decodeAs[contract.WorkspaceView](t, s.must(t, admin.tok, "PATCH", "/api/workspaces/"+defID, `{"name":"Main"}`, 200)); v.PublicID == nil || v.ProxyURL == nil {
		t.Fatalf("admin patch view = %+v", v)
	}
}

func TestAuditLogListsFiltersPagesAndIsAdminOnly(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	for i := range 4 {
		s.must(t, admin.tok, "POST", "/api/groups", `{"name":"g`+string(rune('a'+i))+`"}`, 201)
	}
	code, b := s.call(t, "GET", "/api/audit-log", m.tok, "")
	forbidden(t, code, b, "no matching grant")

	p1 := decodeAs[contract.AuditList](t, s.must(t, admin.tok, "GET", "/api/audit-log?action=group.create&limit=3", "", 200))
	if len(p1.Items) != 3 || p1.NextCursor == nil || p1.Items[0].Action != contract.AuditGroupCreate {
		t.Fatalf("page 1 = %+v", p1)
	}
	p2 := decodeAs[contract.AuditList](t, s.must(t, admin.tok, "GET", "/api/audit-log?action=group.create&limit=3&cursor="+*p1.NextCursor, "", 200))
	if len(p2.Items) != 1 || p2.NextCursor != nil || p2.Items[0].ID == p1.Items[2].ID {
		t.Fatalf("page 2 = %+v", p2)
	}
	for _, q := range []string{"action=workspace.explode", "limit=0", "limit=abc", "limit=201", "cursor=not-a-cursor", "workspaceId=nope"} {
		code, b := s.call(t, "GET", "/api/audit-log?"+q, admin.tok, "")
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
	}
	// every group create carries the actor and no workspace
	if e := p1.Items[0]; e.Actor == nil || e.Actor.UserID != admin.id || e.WorkspaceID != nil || e.ResourceType != "group" {
		t.Fatalf("entry = %+v", e)
	}
}

type creatingAccess struct{ noAccess }

func (creatingAccess) CreateGroup(_ context.Context, name string) (contract.Group, error) {
	return contract.Group{ID: "00000000-0000-0000-0000-0000000000aa", Name: name}, nil
}

// The control plane emits after the change committed: an audit failure is logged at ERROR (without
// the event) and the request still succeeds.
func TestAnAuditFailureDoesNotFailTheChange(t *testing.T) {
	a := &memAudit{err: errors.New("audit store down")}
	f := newFixture(t, func(d *api.Deps) { d.Audit = a; d.Access = creatingAccess{} })
	resp, b := f.do(t, "POST", "/api/groups", `{"name":"x"}`)
	if resp.StatusCode != 201 || !strings.Contains(string(b), `"name":"x"`) {
		t.Fatalf("status %d %s", resp.StatusCode, b)
	}
	if logs := f.logs.String(); !strings.Contains(logs, `"level":"ERROR"`) || !strings.Contains(logs, "group.create") {
		t.Fatalf("the failed append was not logged at ERROR: %s", logs)
	}
	ok := &memAudit{}
	f = newFixture(t, func(d *api.Deps) { d.Audit = ok; d.Access = creatingAccess{} })
	f.do(t, "POST", "/api/groups", `{"name":"y"}`)
	if len(ok.events) != 1 || ok.events[0].Action != contract.AuditGroupCreate || ok.events[0].ActorID != "u-1" || ok.events[0].OrgID != "o-1" || ok.events[0].ResourceID == "" {
		t.Fatalf("events = %+v", ok.events)
	}
}
