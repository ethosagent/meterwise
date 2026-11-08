// Package contract is the shared kernel: the typed error registry, the ports
// every adapter implements, the route-to-permission table and the API DTOs.
// It imports the standard library only (archtest H0), so every other package
// may import it and none of them can create a cycle through it.
//
// Web twins (web/src/contract/generated.ts) and openapi.json are generated
// from this package by cmd/gen; the `generated` gate fails when they drift.
package contract
