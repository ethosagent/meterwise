package store

import (
	"context"
	"time"
)

// RoleCheck is what the connected database role can do that the app role must not (decision on
// docs/runbooks/database-roles.md): bypass every permission, or own the append-only audit table
// and so be able to drop its trigger or its rows. Membership counts whether or not it is inherited:
// a member can always SET ROLE to the owner.
type RoleCheck struct {
	Role         string
	Superuser    bool // is a superuser, or a member of one (SET ROLE reaches it)
	BypassRLS    bool // has BYPASSRLS, directly or through membership
	OwnsAuditLog bool // owns audit_log, or is a member of its owner (inherited or not)
}

const roleCheckSQL = `
SELECT current_user::text,
       EXISTS (SELECT 1 FROM pg_roles r WHERE r.rolsuper AND pg_has_role(current_user, r.rolname, 'MEMBER')),
       EXISTS (SELECT 1 FROM pg_roles r WHERE r.rolbypassrls AND pg_has_role(current_user, r.rolname, 'MEMBER')),
       COALESCE((SELECT bool_or(pg_has_role(current_user, c.relowner, 'MEMBER'))
                   FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                  WHERE c.relname = 'audit_log' AND c.relkind IN ('r', 'p')
                    AND n.nspname <> 'information_schema' AND n.nspname NOT LIKE 'pg\_%'), false)`

// CheckRole reports the privileges of the role this store is connected as. An unmigrated database
// has no audit_log, so OwnsAuditLog is false there. The table is looked up in every schema, not
// only on the search_path, so a DSN with a narrow search_path cannot hide it.
func (s *Store) CheckRole(ctx context.Context) (RoleCheck, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var rc RoleCheck
	if err := s.pool.QueryRow(ctx, roleCheckSQL).Scan(&rc.Role, &rc.Superuser, &rc.BypassRLS, &rc.OwnsAuditLog); err != nil {
		return RoleCheck{}, fail(ClassUnreachable, "store: cannot read the database role")
	}
	return rc, nil
}
