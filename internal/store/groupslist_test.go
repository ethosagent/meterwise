package store_test

import (
	"context"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// An existing group with nothing in it lists as an empty, non-nil slice; an unknown one is not_found;
// a populated one lists exactly its members and grants.
func TestGroupGrantsAndMembersListPlainly(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	s, _, g, _ := groupRig(t)

	grants, err := s.GroupGrants(ctx, org, g.ID)
	if err != nil || grants == nil || len(grants) != 0 {
		t.Fatalf("empty group grants = %v, %v", grants, err)
	}
	members, err := s.GroupMembers(ctx, org, g.ID)
	if err != nil || members == nil || len(members) != 0 {
		t.Fatalf("empty group members = %v, %v", members, err)
	}
	if _, err := s.GroupGrants(ctx, org, missingID); code(err) != contract.CodeNotFound {
		t.Fatalf("unknown group grants: %v", err)
	}
	if _, err := s.GroupMembers(ctx, org, missingID); code(err) != contract.CodeNotFound {
		t.Fatalf("unknown group members: %v", err)
	}
	if _, err := s.GroupGrants(ctx, org, "not-a-uuid"); code(err) != contract.CodeNotFound {
		t.Fatalf("malformed group id: %v", err)
	}

	a := activeUser(t, s, "user_list_a", true)
	if _, err := s.ReplaceGroupMembers(ctx, org, g.ID, []string{a}); err != nil {
		t.Fatal(err)
	}
	if members, err = s.GroupMembers(ctx, org, g.ID); err != nil || len(members) != 1 || members[0].UserID != a {
		t.Fatalf("members = %v, %v", members, err)
	}
	want := []contract.Grant{{ResourceType: "workspace", Action: "read"}}
	if _, err := s.ReplaceGroupGrants(ctx, org, g.ID, want); err != nil {
		t.Fatal(err)
	}
	if grants, err = s.GroupGrants(ctx, org, g.ID); err != nil || len(grants) != 1 || grants[0].ResourceType != "workspace" {
		t.Fatalf("grants = %v, %v", grants, err)
	}
}
