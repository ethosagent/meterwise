# Clerk: setup, invitations, webhooks

Live Clerk is **UNVERIFIED** in this repo: every test uses the `internal/identity/clerkmock` twin (ruling R7).
Treat the steps below as the intended configuration and verify them against a Clerk development instance.

## Instance settings

1. Sign-up mode: **restricted / invitation only**. Meterwise also refuses anyone without an invitation
   (`403 not_invited`), so a mistake here cannot open the org.
2. Customize the **session token** (Sessions, Customize session token) so it carries the claims the adapter reads:

   ```json
   { "email": "{{user.primary_email_address}}",
     "email_verified": "{{user.email_addresses.0.verification.status == 'verified'}}",
     "name": "{{user.full_name}}", "image_url": "{{user.image_url}}" }
   ```

   The exact template expressions for `email_verified` are UNVERIFIED; what the adapter needs is a boolean
   `email_verified` that is true only for a verified primary address. Without it nobody can complete signup.
3. Frontend API URL is the token `iss` (`CLERK_ISSUER`); its `/.well-known/jwks.json` is `CLERK_JWKS_URL`.
4. Webhook endpoint `https://<host>/api/webhooks/clerk`, events `user.created`, `user.updated`, `user.deleted`;
   copy its signing secret to `CLERK_WEBHOOK_SECRET`. Secret key from API keys to `CLERK_SECRET_KEY`.

## First admin

```sh
meterwised migrate
meterwised bootstrap-admin --email you@example.com      # idempotent; -json for scripts
```

This records an admin invitation (7 days) and, with `CLERK_SECRET_KEY` set, asks Clerk to email it. The person
follows the email, signs in, and the web app calls `POST /api/auth/complete-signup`, which turns the invitation
into an active membership. Without Clerk configured (dev) only the database row is created.

## Webhooks without a tunnel

```sh
CLERK_WEBHOOK_SECRET=whsec_... go run ./cmd/clerk-replay -fixture all   # signs committed fixtures, verification ON
```

The fixtures live in `internal/identity/clerkmock/testdata/webhooks`. The login path does not depend on webhooks:
the user row is upserted on the first authenticated request, so a missed webhook never locks anyone out.

## What fails closed

A token with a wrong signature, issuer, expiry or `alg`, or an unknown `kid`, is `401 unauthenticated`. A webhook
with a bad signature, a timestamp more than 5 minutes off, or an already processed message id is `400`. An
unverified email never matches an invitation. A removed membership loses access on the next request (no cache).
