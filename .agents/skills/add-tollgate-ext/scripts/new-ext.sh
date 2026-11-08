#!/usr/bin/env bash
# new-ext.sh <name> — create the failing testkit test for a new extension wiring.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
name="${1:?usage: new-ext.sh <name>   (snake_case)}"
[[ $name =~ ^[a-z][a-z0-9_]*$ ]] || { echo "✗ name must be snake_case" >&2; exit 1; }
[ -d internal/dataplane ] || { echo "✗ internal/dataplane does not exist yet; the data plane plan creates it" >&2; exit 1; }
f="internal/dataplane/ext_${name}_test.go"
[ ! -e "$f" ] || { echo "✗ $f exists" >&2; exit 1; }
cat >"$f" <<GO
package dataplane_test

import "testing"

// Test${name}Wiring drives the proxy built from Deps against testkit.FakeUpstream.
// It must fail first: assert the behaviour the extension adds (status, headers,
// hook calls received), not private fields.
func Test${name}Wiring(t *testing.T) {
	t.Fatal("write the failing testkit test for extension ${name}")
}
GO
echo "$f"
