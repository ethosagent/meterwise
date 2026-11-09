// Package clerkmock is the twin of Clerk (ruling R7): a loopback server that
// publishes a JWKS, signs real RS256 session tokens (and, on request, forged
// ones), records the invitations the adapter creates and revokes, and signs
// Svix webhooks. Tests and the end-to-end run use it instead of a live Clerk
// instance; the committed webhook payloads live in testdata/webhooks.
package clerkmock
