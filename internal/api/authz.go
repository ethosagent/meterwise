package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/contract"
)

// Authz is the authorization the gateway and a few handlers use; *access.Authorizer implements it.
type Authz interface {
	Authorize(ctx context.Context, p contract.Principal, perm contract.Permission, workspaceID string) (access.Result, error)
	// Scope starts the request's view of the caller's grants: any number of Filter calls on it
	// cost one grant read.
	Scope(ctx context.Context, p contract.Principal) *access.GrantView
	Snapshot(ctx context.Context, p contract.Principal) (contract.PermissionSnapshot, error)
}

func errNoRow(method, pattern string) error {
	return fmt.Errorf("api: handler %s %s has no row in the contract route table", method, pattern)
}

// gateway is the one authorization middleware: it authenticates the request, attaches the
// session and, for every route except PermSelf ones, asks the Authorizer whether the caller may
// use the route's permission (on the workspace the route's WorkspaceParam names, if any). It runs
// after tenant binding (the session) and before the handler. A denial is 403 forbidden with
// details.reason; a caller with no membership is 403 not_invited; a workspace outside the org is
// 404; a failure of ours is 500/503, never a denial. Handlers never look at a role.
func (s *server) gateway(row contract.Route, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.Principals.Resolve(r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if row.Permission != contract.PermSelf {
			if !sess.Member {
				writeError(w, contract.NotInvited())
				return
			}
			workspaceID := ""
			switch {
			case row.WorkspaceParam != "":
				workspaceID = r.PathValue(row.WorkspaceParam)
			case row.WorkspaceQuery != "":
				// The path names another resource: the caller says which workspace to authorize
				// against, before any read of that resource.
				if workspaceID = r.URL.Query().Get(row.WorkspaceQuery); workspaceID == "" {
					writeError(w, contract.Invalid(row.WorkspaceQuery, "is required"))
					return
				}
			}
			res, err := s.Authz.Authorize(r.Context(), sess.Principal, row.Permission, workspaceID)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			if !res.Allowed {
				writeError(w, contract.Forbidden("you do not have permission to do this", res.Reason))
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(withSession(r.Context(), sess)))
	})
}

// allowedWorkspaceIDs returns the ids of the workspaces in list on which v grants perm, in list
// order. Every handler that narrows a workspace list by permission goes through it.
func allowedWorkspaceIDs(v *access.GrantView, perm contract.Permission, list []contract.Workspace) ([]string, error) {
	ids := make([]string, len(list))
	for i, w := range list {
		ids[i] = w.ID
	}
	return v.Filter(perm, ids)
}
