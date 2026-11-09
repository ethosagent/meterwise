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
	OrgDefaults(ctx context.Context) ([]contract.Grant, error)
	SetOrgDefaults(ctx context.Context, grants []contract.Grant) ([]contract.Grant, error)
}

func (s *server) listMembers(c Call, _ none) (contract.MemberList, error) {
	list, err := s.Tenancy.ListMembers(c.Ctx, c.Principal)
	return members(list), err
}

func (s *server) updateMember(c Call, req contract.UpdateMemberRequest) (contract.Member, error) {
	return s.Tenancy.SetMemberRole(c.Ctx, c.Principal, c.Path("userId"), req.Role)
}

func (s *server) removeMember(c Call, _ none) (none, error) {
	return none{}, s.Tenancy.RemoveMember(c.Ctx, c.Principal, c.Path("userId"))
}

func (s *server) listGroups(c Call, _ none) (contract.GroupList, error) {
	list, err := s.Access.ListGroups(c.Ctx)
	if list == nil {
		list = []contract.Group{}
	}
	return contract.GroupList{Items: list}, err
}

func (s *server) createGroup(c Call, req contract.GroupRequest) (contract.Group, error) {
	return s.Access.CreateGroup(c.Ctx, req.Name)
}

func (s *server) getGroup(c Call, _ none) (contract.Group, error) {
	return s.Access.Group(c.Ctx, c.Path("id"))
}

func (s *server) renameGroup(c Call, req contract.GroupRequest) (contract.Group, error) {
	return s.Access.RenameGroup(c.Ctx, c.Path("id"), req.Name)
}

func (s *server) deleteGroup(c Call, _ none) (none, error) {
	return none{}, s.Access.DeleteGroup(c.Ctx, c.Path("id"))
}

func (s *server) groupGrants(c Call, _ none) (contract.GrantList, error) {
	list, err := s.Access.GroupGrants(c.Ctx, c.Path("id"))
	return grants(list), err
}

func (s *server) setGroupGrants(c Call, req contract.GrantList) (contract.GrantList, error) {
	list, err := s.Access.SetGroupGrants(c.Ctx, c.Path("id"), req.Grants)
	return grants(list), err
}

func (s *server) attachWorkspace(c Call, req contract.AttachWorkspaceRequest) (contract.Group, error) {
	return s.Access.AttachWorkspace(c.Ctx, c.Path("id"), req.WorkspaceID)
}

func (s *server) detachWorkspace(c Call, _ none) (none, error) {
	return none{}, s.Access.DetachWorkspace(c.Ctx, c.Path("id"), c.Path("wsId"))
}

func (s *server) groupMembers(c Call, _ none) (contract.MemberList, error) {
	list, err := s.Access.GroupMembers(c.Ctx, c.Path("id"))
	return members(list), err
}

func (s *server) addGroupMember(c Call, req contract.AddGroupMemberRequest) (contract.MemberList, error) {
	list, err := s.Access.AddGroupMember(c.Ctx, c.Path("id"), req.UserID)
	return members(list), err
}

func (s *server) removeGroupMember(c Call, _ none) (none, error) {
	return none{}, s.Access.RemoveGroupMember(c.Ctx, c.Path("id"), c.Path("userId"))
}

func (s *server) orgDefaults(c Call, _ none) (contract.GrantList, error) {
	list, err := s.Access.OrgDefaults(c.Ctx)
	return grants(list), err
}

func (s *server) setOrgDefaults(c Call, req contract.GrantList) (contract.GrantList, error) {
	list, err := s.Access.SetOrgDefaults(c.Ctx, req.Grants)
	return grants(list), err
}
