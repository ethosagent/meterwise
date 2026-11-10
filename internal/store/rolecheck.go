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
       COALESCE((SELECT pg_has_role(current_user, c.relowner, 'MEMBER') FROM pg_class c
                  WHERE c.oid = COALESCE(to_regclass('audit_log'), to_regclass('public.audit_log'))), false)`

// CheckRole reports the privileges of the role this store is connected as. An unmigrated database
// has no audit_log, so OwnsAuditLog is false there. The table is found where the app finds it
// (to_regclass on the search_path), then in public, so a DSN with a narrow search_path cannot hide
// the migrated table. A failed read keeps its class: a permissions error is not "unreachable".
func (s *Store) CheckRole(ctx context.Context) (RoleCheck, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var rc RoleCheck
	if err := s.pool.QueryRow(ctx, roleCheckSQL).Scan(&rc.Role, &rc.Superuser, &rc.BypassRLS, &rc.OwnsAuditLog); err != nil {
		return RoleCheck{}, queryErr(err)
	}
	return rc, nil
}
