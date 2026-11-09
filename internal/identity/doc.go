// Package identity is the Clerk adapter behind contract.IdentityProvider
// (decision D3): it verifies session JWTs against a JWKS, maps claims to an
// Identity, creates and revokes invitations through the Clerk backend API and
// verifies Svix-signed webhooks. Nothing here touches the database.
//
// The Clerk SDK and Svix are the only third-party imports (ADR 0013); tests use
// the clerkmock twin, which signs real JWTs against a local JWKS. Live Clerk is
// UNVERIFIED (ruling R7). The session token must carry the custom claims
// documented in docs/runbooks/clerk.md (email, email_verified, name, image_url).
package identity
