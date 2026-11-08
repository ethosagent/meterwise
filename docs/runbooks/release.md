# Release

Meterwise is deployed as one container image: the `meterwised` binary plus the built `web/dist`.

## Before tagging

1. `scripts/run-checks.sh` is green on `main` (CI runs the same script).
2. `git status` is clean and the branch is `main`; the head commit passed CI.
3. Every new migration is a new file; `scripts/run-checks.sh migrate` passes against a copy of production's schema version.
4. `go run ./cmd/gen -check` is clean (generated files are committed).

## Build (what `.github/workflows/release.yml` does)

```sh
go build -trimpath -ldflags "-s -w -X main.version=$VERSION -X main.commit=$(git rev-parse --short HEAD)" -o bin/meterwised ./cmd/meterwised
(cd web && npm ci && npm run build)
```

The workflow builds `linux/amd64` and `linux/arm64`, packs `bin/meterwised` and `web/dist` into one image, and
publishes it only on a `v*` tag pushed by a maintainer. Agents never push tags.

## After

`meterwised version -json` on the running container reports the stamped version and commit. Roll back by redeploying
the previous image; migrations are reversible (`goose down`) but a rollback across a data-changing migration needs a
human decision.
