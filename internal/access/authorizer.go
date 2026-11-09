package access

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
)

// GrantRepository is the only I/O of authorization: every grant that applies to a user, read in
// one round trip.
type GrantRepository interface {
	Grants(ctx context.Context, orgID, userID string) ([]Grant, error)
}

// WorkspaceLookup finds a workspace of an org; any other id is contract.CodeNotFound.
type WorkspaceLookup interface {
	Workspace(ctx context.Context, orgID, id string) (contract.Workspace, error)
}

// Authorizer is what the API gateway calls: it ties the pure evaluator to the grant repository and
// to the workspace check, and loads grants only when the answer depends on them.
type Authorizer struct {
	Repo       GrantRepository
	Workspaces WorkspaceLookup
	Eval       PermissionEvaluator
}

// NewAuthorizer builds the Authorizer over a grant repository and a workspace lookup (the store
// implements both; access imports no I/O package, so the generators can use the taxonomy).
func NewAuthorizer(repo GrantRepository, ws WorkspaceLookup) *Authorizer {
	return &Authorizer{Repo: repo, Workspaces: ws}
}

// grantsFor reads the principal's grants when need is true and nothing otherwise: the one place
// the repository is called, so a request that cannot depend on grants never pays for them.
func (a *Authorizer) grantsFor(ctx context.Context, p contract.Principal, need bool) ([]Grant, error) {
	if !need {
		return nil, nil
	}
	return a.Repo.Grants(ctx, p.OrgID, p.UserID)
}

func subjectOf(p contract.Principal) Subject { return Subject{UserID: p.UserID, Role: p.Role} }

// Authorize decides whether the principal may use perm, on workspaceID when it is not empty.
// A workspace outside the principal's org (or malformed) is contract.CodeNotFound before any grant
// is looked at, so a URL cannot probe another tenant. A denial is a Result, not an error; an
// error is a failure of ours (the database) and must not be read as a denial.
func (a *Authorizer) Authorize(ctx context.Context, p contract.Principal, perm contract.Permission, workspaceID string) (Result, error) {
	if workspaceID != "" {
		if _, err := a.Workspaces.Workspace(ctx, p.OrgID, workspaceID); err != nil {
			return Result{}, err
		}
	}
	req := Request{Subject: subjectOf(p), Permission: perm, WorkspaceID: workspaceID}
	grants, err := a.grantsFor(ctx, p, a.Eval.NeedsGrants(req))
	if err != nil {
		return Result{}, err
	}
	return a.Eval.Evaluate(req, grants), nil
}

// Filter returns the ids among workspaceIDs on which the principal holds perm, in order, with one
// grant read at most.
func (a *Authorizer) Filter(ctx context.Context, p contract.Principal, perm contract.Permission, workspaceIDs []string) ([]string, error) {
	probe := Request{Subject: subjectOf(p), Permission: perm, WorkspaceID: "probe"}
	grants, err := a.grantsFor(ctx, p, a.Eval.NeedsGrants(probe))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(workspaceIDs))
	for _, id := range workspaceIDs {
		if a.Eval.Evaluate(Request{Subject: probe.Subject, Permission: perm, WorkspaceID: id}, grants).Allowed {
			out = append(out, id)
		}
	}
	return out, nil
}

// Snapshot returns the caller's permission snapshot (GET /api/me/permissions).
func (a *Authorizer) Snapshot(ctx context.Context, p contract.Principal) (contract.PermissionSnapshot, error) {
	grants, err := a.grantsFor(ctx, p, !IsAdmin(p.Role))
	if err != nil {
		return contract.PermissionSnapshot{}, err
	}
	return a.Eval.Snapshot(subjectOf(p), grants), nil
}
