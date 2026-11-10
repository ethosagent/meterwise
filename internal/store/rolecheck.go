package store

import (
	"context"
	"time"
)

// RoleCheck is what the connected database role can do that the app role must not (decision on
// docs/runbooks/database-roles.md): bypass every permission, or own the append-only audit table
// and so be able to drop its trigger or its rows.
type RoleCheck struct {
	Role         string
	Superuser    bool
	OwnsAuditLog bool // owns audit_log, or has the owner's privileges through membership
}

const roleCheckSQL = `
SELECT current_user::text,
       COALESCE((SELECT rolsuper FROM pg_roles WHERE rolname = current_user), false),
       COALESCE((SELECT pg_has_role(current_user, c.relowner, 'USAGE')
                   FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                  WHERE c.relname = 'audit_log' AND c.relkind = 'r' AND n.nspname = ANY (current_schemas(false))
                  LIMIT 1), false)`

// CheckRole reports the privileges of the role this store is connected as. An unmigrated database
// has no audit_log, so OwnsAuditLog is false there.
func (s *Store) CheckRole(ctx context.Context) (RoleCheck, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var rc RoleCheck
	if err := s.pool.QueryRow(ctx, roleCheckSQL).Scan(&rc.Role, &rc.Superuser, &rc.OwnsAuditLog); err != nil {
		return RoleCheck{}, fail(ClassUnreachable, "store: cannot read the database role")
	}
	return rc, nil
}
