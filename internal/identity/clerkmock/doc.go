// Package clerkmock is the mock twin of the Clerk identity adapter
// (ruling R7): a contract.IdentityProvider backed by committed fixtures in
// testdata/tokens.json, so every test runs with no Clerk instance and no
// network. Behaviour the real adapter must match (expired token and unknown
// token both answer CodeUnauthorized) is pinned here first.
package clerkmock
