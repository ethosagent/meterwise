# organization

**Settings / Organization**, route `/settings/organization`.

- Shows: the organization name (the deployment's `APP_NAME`), and a **Members** table for anyone with `member:read`.
- Members table: active members plus pending invitations (tag "Pending invite", Resend and Revoke), `Invite member`,
  a role Select (admin | member) and Remove.
- Endpoints: `GET /api/members`, `PATCH|DELETE /api/members/{userId}`, `GET|POST /api/invitations`,
  `POST /api/invitations/{id}/resend`, `DELETE /api/invitations/{id}`.
- Permissions: table needs `member:read`; invite, resend and revoke need `member:manage`; role change and Remove are
  admin-only (PermAdmin). Only an admin may pick the admin role in the invite dialog (escalation guard).
  With `member:manage` but not `member:read` the page shows the invitations table instead.
- Errors: `last_admin`, `already_member` and `duplicate_invitation` have their own messages (`describeApiError`).
- Test: `Page.test.tsx` (admin, read-only member, manage-only member).
