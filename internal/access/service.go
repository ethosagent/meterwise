package access

import (
	"context"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ethosagent/meterwise/internal/contract"
)

const maxGroupNameRunes = 80

// Service is the use-case layer of groups, group membership and org defaults. Authorization of
// who may call it is the gateway's job (the route table); this layer validates input and keeps the
// invariants (taxonomy, scope, one transaction per change).
type Service struct {
	st  Store
	org string
}

// Store is the persistence the service needs; *store.Store implements it.
type Store interface {
	ListGroups(ctx context.Context, orgID string) ([]contract.Group, error)
	Group(ctx context.Context, orgID, id string) (contract.Group, error)
	CreateGroup(ctx context.Context, orgID, name string) (contract.Group, error)
	RenameGroup(ctx context.Context, orgID, id, name string) (contract.Group, error)
	DeleteGroup(ctx context.Context, orgID, id string) error
	AttachGroupWorkspace(ctx context.Context, orgID, groupID, workspaceID string, seed []contract.GrantSpec) (contract.Group, error)
	DetachGroupWorkspace(ctx context.Context, orgID, groupID, workspaceID string) error
	GroupGrants(ctx context.Context, orgID, groupID string) ([]contract.Grant, error)
	ReplaceGroupGrants(ctx context.Context, orgID, groupID string, grants []contract.Grant) ([]contract.Grant, error)
	OrgDefaults(ctx context.Context, orgID string) ([]contract.Grant, error)
	ReplaceOrgDefaults(ctx context.Context, orgID string, grants []contract.Grant) ([]contract.Grant, error)
	GroupMembers(ctx context.Context, orgID, groupID string) ([]contract.Member, error)
	AddGroupMember(ctx context.Context, orgID, groupID, userID string) error
	RemoveGroupMember(ctx context.Context, orgID, groupID, userID string) error
}

// NewService builds the service over a store for one organization.
func NewService(st Store, orgID string) *Service { return &Service{st: st, org: orgID} }

func invalid(field, msg string) error {
	return contract.NewError(contract.CodeValidationFailed, "request validation failed", field+": "+msg)
}

func validGroupName(raw string) (string, error) {
	n := strings.TrimSpace(raw)
	switch {
	case n == "" || utf8.RuneCountInString(n) > maxGroupNameRunes:
		return "", invalid("name", "must be 1-80 characters")
	case strings.IndexFunc(n, unicode.IsControl) >= 0:
		return "", invalid("name", "must not contain control characters")
	}
	return n, nil
}

// ListGroups returns the org's groups.
func (s *Service) ListGroups(ctx context.Context) ([]contract.Group, error) {
	return s.st.ListGroups(ctx, s.org)
}

// Group returns one group.
func (s *Service) Group(ctx context.Context, id string) (contract.Group, error) {
	return s.st.Group(ctx, s.org, id)
}

// CreateGroup creates a group; the name must be free (case-insensitively).
func (s *Service) CreateGroup(ctx context.Context, name string) (contract.Group, error) {
	n, err := validGroupName(name)
	if err != nil {
		return contract.Group{}, err
	}
	return s.st.CreateGroup(ctx, s.org, n)
}

// RenameGroup renames a group.
func (s *Service) RenameGroup(ctx context.Context, id, name string) (contract.Group, error) {
	n, err := validGroupName(name)
	if err != nil {
		return contract.Group{}, err
	}
	return s.st.RenameGroup(ctx, s.org, id, n)
}

// DeleteGroup deletes a group and everything it grants.
func (s *Service) DeleteGroup(ctx context.Context, id string) error {
	return s.st.DeleteGroup(ctx, s.org, id)
}

// AttachWorkspace attaches a group to a workspace and, in the same transaction, seeds the read
// action of every workspace-scoped resource for it. It is idempotent.
func (s *Service) AttachWorkspace(ctx context.Context, groupID, workspaceID string) (contract.Group, error) {
	var seed []contract.GrantSpec
	for _, g := range WorkspaceReadSeed() {
		seed = append(seed, contract.GrantSpec{ResourceType: g.ResourceType, Action: g.Action})
	}
	return s.st.AttachGroupWorkspace(ctx, s.org, groupID, workspaceID, seed)
}

// DetachWorkspace detaches a group and drops its grants on that workspace.
func (s *Service) DetachWorkspace(ctx context.Context, groupID, workspaceID string) error {
	return s.st.DetachGroupWorkspace(ctx, s.org, groupID, workspaceID)
}

// GroupGrants returns a group's grants.
func (s *Service) GroupGrants(ctx context.Context, groupID string) ([]contract.Grant, error) {
	return s.st.GroupGrants(ctx, s.org, groupID)
}

// SetGroupGrants replaces a group's grants. A workspace-scoped resource needs a workspace the
// group is attached to; an org-scoped one needs none (422 otherwise).
func (s *Service) SetGroupGrants(ctx context.Context, groupID string, grants []contract.Grant) ([]contract.Grant, error) {
	if err := ValidateGrants(grants, false); err != nil {
		return nil, err
	}
	return s.st.ReplaceGroupGrants(ctx, s.org, groupID, grants)
}

// OrgDefaults returns the org default grants.
func (s *Service) OrgDefaults(ctx context.Context) ([]contract.Grant, error) {
	return s.st.OrgDefaults(ctx, s.org)
}

// SetOrgDefaults replaces the org defaults. A null workspace means every workspace; an org-scoped
// resource must have a null workspace.
func (s *Service) SetOrgDefaults(ctx context.Context, grants []contract.Grant) ([]contract.Grant, error) {
	if err := ValidateGrants(grants, true); err != nil {
		return nil, err
	}
	return s.st.ReplaceOrgDefaults(ctx, s.org, grants)
}

// GroupMembers lists a group's members.
func (s *Service) GroupMembers(ctx context.Context, groupID string) ([]contract.Member, error) {
	return s.st.GroupMembers(ctx, s.org, groupID)
}

// AddGroupMember adds an active member to a group (group_users only) and returns the group's members.
func (s *Service) AddGroupMember(ctx context.Context, groupID, userID string) ([]contract.Member, error) {
	if err := s.st.AddGroupMember(ctx, s.org, groupID, userID); err != nil {
		return nil, err
	}
	return s.st.GroupMembers(ctx, s.org, groupID)
}

// RemoveGroupMember takes a user out of a group.
func (s *Service) RemoveGroupMember(ctx context.Context, groupID, userID string) error {
	return s.st.RemoveGroupMember(ctx, s.org, groupID, userID)
}

// ValidateGrants checks a grant list against the taxonomy: known resource type and action, and the
// scope rule (workspace-scoped needs a workspace id unless allowAllWorkspaces, org-scoped needs
// none), with no duplicates. Every problem is listed, never just the first.
func ValidateGrants(grants []contract.Grant, allowAllWorkspaces bool) error {
	var problems []string
	seen := map[string]bool{}
	for i, g := range grants {
		at := "grants[" + strconv.Itoa(i) + "]"
		scope, known := ScopeOf(g.ResourceType)
		switch {
		case !known:
			problems = append(problems, at+": unknown resourceType "+strconv.Quote(g.ResourceType))
			continue
		case !Valid(g.ResourceType, g.Action):
			problems = append(problems, at+": unknown action "+strconv.Quote(g.Action)+" for "+g.ResourceType)
			continue
		case scope == ScopeOrg && g.WorkspaceID != nil:
			problems = append(problems, at+": "+g.ResourceType+" is org-scoped; workspaceId must be null")
		case scope == ScopeWorkspace && g.WorkspaceID == nil && !allowAllWorkspaces:
			problems = append(problems, at+": "+g.ResourceType+" is workspace-scoped; workspaceId is required")
		}
		key := g.ResourceType + "\x00" + g.Action + "\x00" + deref(g.WorkspaceID)
		if seen[key] {
			problems = append(problems, at+": duplicate grant")
		}
		seen[key] = true
	}
	if len(problems) == 0 {
		return nil
	}
	return contract.NewError(contract.CodeValidationFailed, "request validation failed", problems...)
}
