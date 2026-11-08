// Command gen regenerates every derived artifact from its source of truth and,
// with -check, fails when a file on disk differs (the `generated` gate):
//
//	contract-ts  web/src/contract/generated.ts  from internal/contract
//	openapi      internal/api/openapi.json      from the route table and DTOs
//	schema       docs/schema.md                 from internal/store/migrations
//	migrations   lints migrations: one feature per file, never a foreign table
//
// Usage: gen [-check] [-root dir] [all|contract-ts|openapi|schema|migrations]
package main
