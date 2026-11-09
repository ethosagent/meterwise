package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

const (
	idOne   = "AAAAAAAAAAAAAAAAAAAAAA"
	idTwo   = "BBBBBBBBBBBBBBBBBBBBBB"
	idThree = "CCCCCCCCCCCCCCCCCCCCCC"
)

func TestResetKeepsTheOldIDAnsweringUntilItsGraceEnds(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	s, conn := auditRig(t)
	ws, err := s.CreateWorkspace(ctx, org, "Alpha", idOne, 9)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Microsecond)

	got, until, err := s.ResetWorkspaceURL(ctx, org, ws.ID, idTwo, 10*time.Minute, now)
	if err != nil || got.PublicID != idTwo || !until.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("reset: %+v %v %v", got, until, err)
	}
	cur, err := s.WorkspaceByPublicID(ctx, idTwo, now)
	if err != nil || cur.Retired || cur.ID != ws.ID {
		t.Fatalf("new id: %+v %v", cur, err)
	}
	old, err := s.WorkspaceByPublicID(ctx, idOne, now.Add(9*time.Minute))
	if err != nil || !old.Retired || old.PublicID != idOne || old.GraceUntil == nil || !old.GraceUntil.Equal(until) || old.ID != ws.ID {
		t.Fatalf("old id in grace: %+v %v", old, err)
	}
	if _, err := s.WorkspaceByPublicID(ctx, idOne, until); code(err) != contract.CodeWorkspaceURLReset {
		t.Fatalf("old id at the end of grace: %v", err)
	}
	if _, err := s.WorkspaceByPublicID(ctx, idThree, now); code(err) != contract.CodeNotFound {
		t.Fatalf("never issued: %v", err)
	}
	if w, _ := s.Workspace(ctx, org, ws.ID); w.PublicID != idTwo {
		t.Fatalf("workspaces.public_id = %q", w.PublicID)
	}

	// a second reset: the first retired id keeps its own window, the one just replaced gets a new one
	if _, _, err := s.ResetWorkspaceURL(ctx, org, ws.ID, idThree, 0, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if o, err := s.WorkspaceByPublicID(ctx, idOne, now.Add(2*time.Minute)); err != nil || !o.Retired {
		t.Fatalf("an earlier retired id kept its window: %+v %v", o, err)
	}
	if _, err := s.WorkspaceByPublicID(ctx, idTwo, now.Add(time.Minute)); code(err) != contract.CodeWorkspaceURLReset {
		t.Fatalf("grace 0 ends at once: %v", err)
	}
	var current int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM workspace_public_ids WHERE workspace_id = $1 AND retired_at IS NULL`, ws.ID).Scan(&current); err != nil || current != 1 {
		t.Fatalf("current ids = %d %v", current, err)
	}
	// the database itself refuses a second current id
	if _, err := conn.Exec(ctx, `INSERT INTO workspace_public_ids (public_id, org_id, workspace_id) VALUES ('DDDDDDDDDDDDDDDDDDDDDD', $1, $2)`, org, ws.ID); err == nil {
		t.Fatal("a second current public id was accepted")
	}

	n, err := s.EndPreviousURLGrace(ctx, org, ws.ID, now.Add(3*time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("end grace: %d %v", n, err)
	}
	if _, err := s.WorkspaceByPublicID(ctx, idOne, now.Add(3*time.Minute)); code(err) != contract.CodeWorkspaceURLReset {
		t.Fatalf("after ending the grace: %v", err)
	}
	if n, err := s.EndPreviousURLGrace(ctx, org, ws.ID, now.Add(3*time.Minute)); err != nil || n != 0 {
		t.Fatalf("ending twice is a no-op: %d %v", n, err)
	}
}

func TestResetCollisionAndScopingErrors(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	s, _ := auditRig(t)
	a, err := s.CreateWorkspace(ctx, org, "A", idOne, 9)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateWorkspace(ctx, org, "B", idTwo, 9)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, _, err := s.ResetWorkspaceURL(ctx, org, a.ID, idTwo, time.Minute, now); !errors.Is(err, store.ErrPublicIDTaken) {
		t.Fatalf("collision: %v", err)
	}
	if w, _ := s.Workspace(ctx, org, a.ID); w.PublicID != idOne {
		t.Fatalf("a failed reset must change nothing, got %q", w.PublicID)
	}
	if cur, err := s.WorkspaceByPublicID(ctx, idOne, now); err != nil || cur.Retired {
		t.Fatalf("the old id is still current after the failed reset: %+v %v", cur, err)
	}
	for _, id := range []string{missingID, "not-a-uuid"} {
		if _, _, err := s.ResetWorkspaceURL(ctx, org, id, idThree, time.Minute, now); code(err) != contract.CodeNotFound {
			t.Errorf("reset of %s: %v", id, err)
		}
		if _, err := s.EndPreviousURLGrace(ctx, org, id, now); code(err) != contract.CodeNotFound {
			t.Errorf("end grace of %s: %v", id, err)
		}
	}
	if _, _, err := s.ResetWorkspaceURL(ctx, missingID, b.ID, idThree, time.Minute, now); code(err) != contract.CodeNotFound {
		t.Errorf("another org's workspace: %v", err)
	}
}

func TestAllowedClientCIDRsRoundTrip(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	s, _ := auditRig(t)
	ws, err := s.CreateWorkspace(ctx, org, "A", idOne, 9)
	if err != nil {
		t.Fatal(err)
	}
	if ws.AllowedClientCIDRs == nil || len(ws.AllowedClientCIDRs) != 0 || ws.LastCallAt != nil {
		t.Fatalf("defaults: %+v", ws)
	}
	got, err := s.SetAllowedClientCIDRs(ctx, org, ws.ID, []string{"10.0.0.0/8", "203.0.113.7/32", "2001:db8::/32"})
	if err != nil || len(got) != 3 || got[1] != "203.0.113.7/32" {
		t.Fatalf("set: %v %v", got, err)
	}
	if w, _ := s.WorkspaceByPublicID(ctx, idOne, time.Now()); len(w.AllowedClientCIDRs) != 3 || w.AllowedClientCIDRs[0] != "10.0.0.0/8" {
		t.Fatalf("the data-plane lookup carries the CIDRs: %+v", w.AllowedClientCIDRs)
	}
	if got, err = s.SetAllowedClientCIDRs(ctx, org, ws.ID, nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("clear: %#v %v", got, err)
	}
	if _, err := s.SetAllowedClientCIDRs(ctx, missingID, ws.ID, nil); code(err) != contract.CodeNotFound {
		t.Fatalf("another org: %v", err)
	}
}
