// Command meterwised is the one Meterwise binary (decision D1). This build
// carries the subcommand skeleton: version, config check and serve (health
// and version endpoints) work; migrate and bootstrap-admin answer a typed
// not_available error until the store lands. Every subcommand accepts -json
// and then prints exactly one JSON document: {"ok":true,"data":...} or
// {"ok":false,"error":{"code","message","details"}}.
package main
