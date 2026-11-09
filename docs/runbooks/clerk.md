# Clerk: setup, invitations, webhooks

Live Clerk is **UNVERIFIED** in this repo: every test uses the `internal/identity/clerkmock` twin (ruling R7).
Treat the steps below as the intended configuration and verify them against a Clerk development instance.

## Instance settings

1. Sign-up mode: **restricted / invitation only**. Meterwise also refuses anyone without an invitation
   (`403 not_invited`), so a mistake here cannot open the org.
2. Customize the **session token** (Sessions, Customize session token) so it carries the claims the adapter reads.
   The email must be the user's **primary** address, never `email_addresses.0` (the first address in the list is not
   necessarily the primary one, nor the verified one), and `email_verified` must be a JSON **boolean** that is true only
   when that same primary address is verified:

   ```json
   { "email": "{{user.primary_email_address}}",
     "email_verified": "{{user.primary_email_address_verified}}",
     "name": "{{user.full_name}}", "image_url": "{{user.image_url}}" }
   ```

   UNVERIFIED against a live instance: the shortcode for the primary address's verification state, and whether Clerk
   renders it as a JSON boolean or as the string `"true"`. The adapter decodes `email_verified` as a boolean, so a
   string, a missing claim or a verification state taken from another address fails closed (nobody can complete
   signup; login of an already linked user is unaffected). After saving the template, decode one real token and check
   `email` is the primary address and `"email_verified": true` is a boolean before inviting anyone.
   The adapter also refuses a token with no `exp`.
3. Frontend API URL is the token `iss` (`CLERK_ISSUER`); its `/.well-known/jwks.json` is `CLERK_JWKS_URL`.
4. Webhook endpoint `https://<host>/api/webhooks/clerk`, events `user.created`, `user.updated`, `user.deleted`;
   copy its signing secret to `CLERK_WEBHOOK_SECRET`. Secret key from API keys to `CLERK_SECRET_KEY`.
5. `CLERK_AUTHORIZED_PARTIES`: comma-separated origins of the web app (for example `https://app.example.com`), the
   values Clerk puts in the token's `azp` claim. It is **required outside `APP_ENV=dev`** and `meterwised config check`
   reports it with the other missing settings. A token whose `azp` is absent or not listed is refused. Origins carry no
   path or credentials; a trailing slash is dropped.

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
