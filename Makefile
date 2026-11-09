# Thin wrappers. The logic lives in scripts/ so make, hooks and CI run the same thing.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)

.PHONY: build test check fast fmt lint hooks run migrate sqlc gen web db-up db-down worktree demo-traffic clean

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/meterwised ./cmd/meterwised

test:
	scripts/run-checks.sh test

check:
	scripts/run-checks.sh

fast:
	scripts/run-checks.sh --fast

fmt:
	gofmt -w .

lint:
	scripts/run-checks.sh lint

hooks:
	scripts/install-hooks.sh

run:
	go run ./cmd/meterwised serve

migrate:
	go run ./cmd/meterwised migrate

sqlc:
	bash -c '. scripts/lib.sh && gotool sqlc generate'

web:
	cd web && npm ci --no-audit --no-fund && npm run build

gen:
	go run ./cmd/gen

db-up:
	METERWISE_PG_PORT=$${METERWISE_PG_PORT:-54329} docker compose up -d --wait postgres

db-down:
	docker compose down

# make worktree PLAN=05  -> ../meterwise-05 on branch wave/05
worktree:
	@test -n "$(PLAN)" || { echo "usage: make worktree PLAN=05" >&2; exit 2; }
	git worktree add ../meterwise-$(PLAN) -b wave/$(PLAN)
	cd ../meterwise-$(PLAN) && scripts/install-hooks.sh

demo-traffic:
	.agents/skills/demo-traffic/scripts/replay.sh

clean:
	rm -rf bin .logs web/dist web/coverage
