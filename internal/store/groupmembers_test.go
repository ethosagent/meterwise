package store_test

import (
	"context"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

func activeUser(t *testing.T, s *store.Store, sub string, active bool) string {
	t.Helper()
	ctx, org := context.Background(), store.OrgID()
	u, err := s.UpsertUser(ctx, org, contract.Identity{Subject: sub, Email: sub + "@corp.test", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	if active {
		if err := s.UpsertActiveMembership(ctx, org, u.ID, contract.RoleMember); err != nil {
			t.Fatal(err)
		}
	}
	return u.ID
}

func memberIDs(list []contract.Member) map[string]bool {
	out := map[string]bool{}
	for _, m := range list {
		out[m.UserID] = true
	}
	return out
}

func TestReplaceGroupMembersIsOneAllOrNothingSwap(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	s, _, g, _ := groupRig(t)
	a, b, c := activeUser(t, s, "user_a", true), activeUser(t, s, "user_b", true), activeUser(t, s, "user_c", true)
	outsider := activeUser(t, s, "user_out", false)

	got, err := s.ReplaceGroupMembers(ctx, org, g.ID, []string{a, b, a})
	if err != nil || len(got) != 2 || !memberIDs(got)[a] || !memberIDs(got)[b] {
		t.Fatalf("first set: %+v %v", got, err)
	}
	got, err = s.ReplaceGroupMembers(ctx, org, g.ID, []string{b, c})
	if err != nil || len(got) != 2 || memberIDs(got)[a] || !memberIDs(got)[c] {
		t.Fatalf("swap: %+v %v", got, err)
	}
	// one id that is not an active member: 422 and nothing changes
	if _, err := s.ReplaceGroupMembers(ctx, org, g.ID, []string{a, outsider}); code(err) != contract.CodeValidationFailed {
		t.Fatalf("non-member: %v", err)
	}
	for _, bad := range []string{"not-a-uuid", missingID} {
		if _, err := s.ReplaceGroupMembers(ctx, org, g.ID, []string{b, bad}); code(err) != contract.CodeValidationFailed {
			t.Fatalf("bad id %q: %v", bad, err)
		}
	}
	after, err := s.GroupMembers(ctx, org, g.ID)
	if err != nil || len(after) != 2 || !memberIDs(after)[b] || !memberIDs(after)[c] {
		t.Fatalf("a refused swap changed the set: %+v %v", after, err)
	}
	if _, err := s.ReplaceGroupMembers(ctx, org, missingID, []string{a}); code(err) != contract.CodeNotFound {
		t.Fatalf("unknown group: %v", err)
	}
	if got, err = s.ReplaceGroupMembers(ctx, org, g.ID, nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("clear: %#v %v", got, err)
	}
}
