// Command clerkmock-serve runs the Clerk twin as a standalone local issuer, for
// the real end-to-end run of the control plane and the web app (never production):
//
//	go run ./cmd/clerkmock-serve -ca-out /tmp/clerkmock-ca.pem
//
// It serves what a Clerk instance serves over HTTPS (the JWKS and the backend
// invitations API, with a self-signed certificate it writes to -ca-out so the
// process under test can trust it through SSL_CERT_FILE) and, on a plain HTTP
// loopback port, a token mint for the dev-only web mock adapter:
//
//	GET  /mint?email=a@b.test&name=Ada   -> {"token": "<signed RS256 JWT>"}
//	POST /admin/expire?email=a@b.test    -> later mints for that email are expired
//	POST /admin/restore?email=a@b.test   -> mints are valid again
//	GET  /admin/state                    -> invitations the control plane created
//
// On start it prints one JSON object with the values to export for meterwised.
package main
