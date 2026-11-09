# organization

**Settings / Organization** (`/settings/organization`).

- Shows: the organization name (the deployment's `appName`; the API has no organization endpoint yet),
  and for admins the **Invitations** table: `＋ Invite member` (email, role admin or member), Resend and
  Revoke on pending rows. A `409 duplicate_invitation` is explained in the dialog.
- Out of scope: the Members table (Plan 03).
- Endpoints: `GET|POST /api/invitations`, `POST /api/invitations/{id}/resend`, `DELETE /api/invitations/{id}`.
- Permission: `invitations.manage`. A member sees the name only and the page never requests invitations.
- Tests: `Page.test.tsx` (admin flows, member view) against `test/mockApi.ts`.
