package api

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Access is what the group, group-member and org-default handlers need from the access layer;
// *access.Service implements it.
type Access interface {
	ListGroups(ctx context.Context) ([]contract.Group, error)
	Group(ctx context.Context, id string) (contract.Group, error)
	CreateGroup(ctx context.Context, name string) (contract.Group, error)
	RenameGroup(ctx context.Context, id, name string) (contract.Group, error)
	DeleteGroup(ctx context.Context, id string) error
	AttachWorkspace(ctx context.Context, groupID, workspaceID string) (contract.Group, error)
	DetachWorkspace(ctx context.Context, groupID, workspaceID string) error
	GroupGrants(ctx context.Context, groupID string) ([]contract.Grant, error)
	SetGroupGrants(ctx context.Context, groupID string, grants []contract.Grant) ([]contract.Grant, error)
	GroupMembers(ctx context.Context, groupID string) ([]contract.Member, error)
	AddGroupMember(ctx context.Context, groupID, userID string) ([]contract.Member, error)
	RemoveGroupMember(ctx context.Context, groupID, userID string) error
	SetGroupMembers(ctx context.Context, groupID string, userIDs []string) ([]contract.Member, error)
	OrgDefaults(ctx context.Context) ([]contract.Grant, error)
	SetOrgDefaults(ctx context.Context, grants []contract.Grant) ([]contract.Grant, error)
}

func (s *server) listGroups(c Call, _ none) (contract.GroupList, error) {
	list, err := s.Access.ListGroups(c.Ctx)
	return contract.GroupList{Items: list}, err
}

func (s *server) createGroup(c Call, req contract.GroupRequest) (contract.Group, error) {
	g, err := s.Access.CreateGroup(c.Ctx, req.Name)
	return g, err
}

func (s *server) getGroup(c Call, _ none) (contract.Group, error) {
	return s.Access.Group(c.Ctx, c.Path("id"))
}

func (s *server) renameGroup(c Call, req contract.GroupRequest) (contract.Group, error) {
	g, err := s.Access.RenameGroup(c.Ctx, c.Path("id"), req.Name)
	return g, err
}

func (s *server) deleteGroup(c Call, _ none) (none, error) {
	if err := s.Access.DeleteGroup(c.Ctx, c.Path("id")); err != nil {
		return none{}, err
	}
	return none{}, nil
}

func (s *server) groupGrants(c Call, _ none) (contract.GrantList, error) {
	list, err := s.Access.GroupGrants(c.Ctx, c.Path("id"))
	return contract.GrantList{Grants: list}, err
}

func (s *server) setGroupGrants(c Call, req contract.GrantList) (contract.GrantList, error) {
	list, err := s.Access.SetGroupGrants(c.Ctx, c.Path("id"), req.Grants)
	return contract.GrantList{Grants: list}, err
}

func (s *server) attachWorkspace(c Call, req contract.AttachWorkspaceRequest) (contract.Group, error) {
	g, err := s.Access.AttachWorkspace(c.Ctx, c.Path("id"), req.WorkspaceID)
	return g, err
}

func (s *server) detachWorkspace(c Call, _ none) (none, error) {
	if err := s.Access.DetachWorkspace(c.Ctx, c.Path("id"), c.Path("wsId")); err != nil {
		return none{}, err
	}
	return none{}, nil
}

func (s *server) groupMembers(c Call, _ none) (contract.MemberList, error) {
	list, err := s.Access.GroupMembers(c.Ctx, c.Path("id"))
	return contract.MemberList{Items: list}, err
}

func (s *server) addGroupMember(c Call, req contract.AddGroupMemberRequest) (contract.MemberList, error) {
	list, err := s.Access.AddGroupMember(c.Ctx, c.Path("id"), req.UserID)
	return contract.MemberList{Items: list}, err
}

func (s *server) setGroupMembers(c Call, req contract.ReplaceGroupMembersRequest) (contract.MemberList, error) {
	list, err := s.Access.SetGroupMembers(c.Ctx, c.Path("id"), req.UserIDs)
	return contract.MemberList{Items: list}, err
}

func (s *server) removeGroupMember(c Call, _ none) (none, error) {
	if err := s.Access.RemoveGroupMember(c.Ctx, c.Path("id"), c.Path("userId")); err != nil {
		return none{}, err
	}
	return none{}, nil
}

func (s *server) orgDefaults(c Call, _ none) (contract.GrantList, error) {
	list, err := s.Access.OrgDefaults(c.Ctx)
	return contract.GrantList{Grants: list}, err
}

func (s *server) setOrgDefaults(c Call, req contract.GrantList) (contract.GrantList, error) {
	list, err := s.Access.SetOrgDefaults(c.Ctx, req.Grants)
	return contract.GrantList{Grants: list}, err
}
