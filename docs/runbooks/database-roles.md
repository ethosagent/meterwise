# Database roles

Two roles, two jobs. Outside `APP_ENV=dev`, `meterwised serve` **refuses to start** if the role in
`DATABASE_URL` is a superuser, is a **member** of a superuser role, has `BYPASSRLS` (directly or through
membership), or owns `audit_log` (it names the role and points here). A superuser
bypasses every permission, a member of one can `SET ROLE` to it, and an owner may drop the append-only trigger of ADR 0018, so either one
could rewrite the audit trail.

| Role | Used by | Can |
|---|---|---|
| `meterwise_owner` | `meterwised migrate`, run by the deploy pipeline | own the schema, create and alter tables |
| `meterwise_app` | `meterwised serve` | `SELECT`, `INSERT` on `audit_log`; `SELECT`, `INSERT`, `UPDATE`, `DELETE` on every other table |

`meterwise_app` is not a superuser, is in no superuser or `BYPASSRLS` role, does not own any table, and has no `UPDATE` or `DELETE` on
`audit_log`.

## One-time setup (as a superuser, once per database)

```sql
CREATE ROLE meterwise_owner LOGIN PASSWORD '<set from your secret store>';
CREATE ROLE meterwise_app   LOGIN PASSWORD '<set from your secret store>';
CREATE DATABASE meterwise OWNER meterwise_owner;
\c meterwise
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO meterwise_app;
GRANT CREATE ON SCHEMA public TO meterwise_owner;
-- tables the owner creates later get these rights automatically
ALTER DEFAULT PRIVILEGES FOR ROLE meterwise_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO meterwise_app;
ALTER DEFAULT PRIVILEGES FOR ROLE meterwise_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO meterwise_app;
```

## After the migrations create `audit_log`

The default privileges above would give the app `UPDATE`/`DELETE` on `audit_log`; take them away
once the table exists (run again after any migration that recreates it):

```sql
REVOKE UPDATE, DELETE, TRUNCATE ON audit_log FROM meterwise_app;
GRANT  SELECT, INSERT          ON audit_log TO   meterwise_app;
```

## Running

```sh
DATABASE_URL=postgres://meterwise_owner:...@db/meterwise meterwised migrate
DATABASE_URL=postgres://meterwise_app:...@db/meterwise    meterwised serve
```

## Checking

```sql
SELECT rolname, rolsuper, rolbypassrls FROM pg_roles WHERE rolname IN ('meterwise_owner','meterwise_app');
SELECT r.rolname FROM pg_roles r                                      -- no rows
 WHERE (r.rolsuper OR r.rolbypassrls) AND pg_has_role('meterwise_app', r.rolname, 'MEMBER');
SELECT tableowner FROM pg_tables WHERE tablename = 'audit_log';      -- meterwise_owner
SELECT has_table_privilege('meterwise_app', 'audit_log', 'UPDATE');  -- false
```

Group membership counts, inherited or not (`GRANT meterwise_owner TO meterwise_app WITH INHERIT FALSE`, or a `NOINHERIT` role, can still `SET ROLE` to the owner): if `meterwise_app` is a member of `meterwise_owner` (or of any role that
owns `audit_log`) the guard treats it as an owner and refuses; membership of any superuser role
(`GRANT postgres TO meterwise_app`) or of any `BYPASSRLS` role is refused the same way. In dev (`APP_ENV=dev`) the guard is
off so the compose Postgres superuser works.

Rotating the passwords: see [`rotate-keys.md`](rotate-keys.md).
