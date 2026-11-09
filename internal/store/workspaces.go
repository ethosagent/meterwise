package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// wsRow is the column set every workspace query returns.
type wsRow struct {
	ID, OrgID, PublicID, Name string
	IsDefault                 bool
	Settings                  []byte
	CreatedAt                 pgtype.Timestamptz
}

func (r wsRow) workspace() contract.Workspace {
	return contract.Workspace{
		ID: r.ID, OrgID: r.OrgID, PublicID: r.PublicID, Name: r.Name, IsDefault: r.IsDefault,
		Settings: settingsOf(r.Settings), CreatedAt: ts(r.CreatedAt),
	}
}

// ListWorkspaces returns the org's workspaces, default first.
func (s *Store) ListWorkspaces(ctx context.Context, orgID string) ([]contract.Workspace, error) {
	rows, err := s.q.ListWorkspaces(ctx, orgID)
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.Workspace, 0, len(rows))
	for _, r := range rows {
		out = append(out, wsRow(r).workspace())
	}
	return out, nil
}

// Workspace returns one workspace of the org; any other id, including one that
// belongs to another org or is malformed, is CodeNotFound.
func (s *Store) Workspace(ctx context.Context, orgID, id string) (contract.Workspace, error) {
	if !validID(id) {
		return contract.Workspace{}, errNotFound
	}
	r, err := s.q.GetWorkspace(ctx, sqlc.GetWorkspaceParams{OrgID: orgID, ID: id})
	if err != nil {
		return contract.Workspace{}, queryErr(err)
	}
	return wsRow(r).workspace(), nil
}

// WorkspaceByID is the data-plane lookup: it is not scoped to an org, and the
// returned OrgID is what the caller checks.
func (s *Store) WorkspaceByID(ctx context.Context, id string) (contract.Workspace, error) {
	if !validID(id) {
		return contract.Workspace{}, errNotFound
	}
	r, err := s.q.GetWorkspaceByID(ctx, id)
	if err != nil {
		return contract.Workspace{}, queryErr(err)
	}
	return wsRow(r).workspace(), nil
}

// WorkspaceByPublicID resolves a current or retired public id. A retired id
// inside its grace window returns Retired=true; past it, or unknown, CodeNotFound.
func (s *Store) WorkspaceByPublicID(ctx context.Context, publicID string, now time.Time) (contract.Workspace, error) {
	r, err := s.q.GetWorkspaceByPublicID(ctx, sqlc.GetWorkspaceByPublicIDParams{PublicID: publicID, Now: pgts(now)})
	if err != nil {
		return contract.Workspace{}, queryErr(err)
	}
	w := wsRow{ID: r.ID, OrgID: r.OrgID, PublicID: r.PublicID, Name: r.Name, IsDefault: r.IsDefault, Settings: r.Settings, CreatedAt: r.CreatedAt}.workspace()
	w.Retired = r.Retired
	return w, nil
}

// CreateWorkspace inserts a workspace and its public id when the org holds fewer
// than max workspaces (CodeWorkspaceLimit otherwise). Concurrent creates
// serialise on an advisory lock, so the limit cannot be overshot. A public id
// collision returns ErrPublicIDTaken so the caller can draw another.
func (s *Store) CreateWorkspace(ctx context.Context, orgID, name, publicID string, limit int) (contract.Workspace, error) {
	var out contract.Workspace
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := q.LockWorkspaceCreation(ctx, orgID); err != nil {
			return err
		}
		n, err := q.CountWorkspaces(ctx, orgID)
		if err != nil {
			return err
		}
		if int(n) >= limit {
			return contract.NewError(contract.CodeWorkspaceLimit, "the organization has reached its workspace limit")
		}
		r, err := q.InsertWorkspace(ctx, sqlc.InsertWorkspaceParams{OrgID: orgID, PublicID: publicID, Name: name})
		if err != nil {
			return err
		}
		if err := q.InsertPublicID(ctx, sqlc.InsertPublicIDParams{PublicID: publicID, OrgID: orgID, WorkspaceID: r.ID}); err != nil {
			return err
		}
		out = wsRow(r).workspace()
		return nil
	})
	if isUnique(err) {
		return contract.Workspace{}, ErrPublicIDTaken
	}
	return out, queryErr(err)
}

// ErrPublicIDTaken reports a public id collision on insert.
var ErrPublicIDTaken = fail(ClassPool, "store: public id already taken")

// EnsureDefaultWorkspace creates the org's default workspace when none exists
// and reports whether it did.
func (s *Store) EnsureDefaultWorkspace(ctx context.Context, orgID, name, publicID string) (bool, error) {
	created := false
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := q.LockWorkspaceCreation(ctx, orgID); err != nil {
			return err
		}
		id, err := q.InsertDefaultWorkspace(ctx, sqlc.InsertDefaultWorkspaceParams{OrgID: orgID, PublicID: publicID, Name: name})
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return err
		}
		created = true
		return q.InsertPublicID(ctx, sqlc.InsertPublicIDParams{PublicID: publicID, OrgID: orgID, WorkspaceID: id})
	})
	if isUnique(err) {
		return false, ErrPublicIDTaken
	}
	return created, queryErr(err)
}

// UpdateWorkspace changes the name and/or settings of an org workspace.
func (s *Store) UpdateWorkspace(ctx context.Context, orgID, id string, name *string, settings map[string]any) (contract.Workspace, error) {
	if !validID(id) {
		return contract.Workspace{}, errNotFound
	}
	p := sqlc.UpdateWorkspaceParams{OrgID: orgID, ID: id}
	if name != nil {
		p.Name = pgtype.Text{String: *name, Valid: true}
	}
	if settings != nil {
		b, err := json.Marshal(settings)
		if err != nil {
			return contract.Workspace{}, fail(ClassPool, "store: settings are not JSON")
		}
		p.Settings = b
	}
	r, err := s.q.UpdateWorkspace(ctx, p)
	if err != nil {
		return contract.Workspace{}, queryErr(err)
	}
	return wsRow(r).workspace(), nil
}

// DeleteWorkspace deletes a non-default workspace of the org. CodeNotFound for an
// unknown id, CodeDefaultWorkspace for the default one.
func (s *Store) DeleteWorkspace(ctx context.Context, orgID, id string) error {
	w, err := s.Workspace(ctx, orgID, id)
	if err != nil {
		return err
	}
	if w.IsDefault {
		return contract.NewError(contract.CodeDefaultWorkspace, "the default workspace cannot be deleted")
	}
	n, err := s.q.DeleteWorkspace(ctx, sqlc.DeleteWorkspaceParams{OrgID: orgID, ID: id})
	if err != nil {
		return queryErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

// Org returns the one organization.
func (s *Store) Org(ctx context.Context) (contract.Org, error) {
	r, err := s.q.GetOrganization(ctx, orgID)
	if err != nil {
		return contract.Org{}, queryErr(err)
	}
	return contract.Org{ID: r.ID, Name: r.Name}, nil
}
