# Rotating keys and secrets

Meterwise holds few secrets. It never stores customer provider keys (they pass through the proxy untouched).

| Secret | Where it lives | Rotate when | How |
|---|---|---|---|
| Clerk secret key and JWKS URL | deployment environment | suspected leak, staff change | create the new key in Clerk, deploy it, revoke the old key |
| Database password | deployment environment (`METERWISE_DATABASE_URL`) | suspected leak, quarterly | create a second role/password, deploy, drop the old one |
| Workspace public id | `workspace_public_ids` table | the URL leaked or was shared too widely | the workspace admin uses Reset URL; the old id answers `gone` for the grace window |
| Svix webhook secret | deployment environment | suspected leak | rotate in Svix, deploy, then retire the old one |

## Rules

- Secrets never enter the repo, fixtures, logs or the PR text. `config check` masks the database password.
- Test fixtures use obviously fake values (`mock-admin-token`, `demo-not-a-real-key`).
- After a rotation, run `meterwised config check -json` in the target environment and check `/healthz`.
- If a provider key appears in a log or table, treat it as a security incident: purge the data, tell the customer, fix the leak (`PRIV-2`).
