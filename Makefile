# Self-documenting: `make` prints `make help`. Logic lives in scripts/ and deploy/dev/ so make, hooks and CI run the same thing.
# Settings come from .env (make setup creates it from .env.example); anything can be overridden on the command line.
-include .env

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)

METERWISE_PG_PORT ?= 54329
DATABASE_URL      ?= postgres://meterwise:meterwise@127.0.0.1:54329/meterwise?sslmode=disable
APP_PG_PORT       ?= 54330
APP_API_PORT      ?= 8081
APP_PROXY_PORT    ?= 8080
APP_ADMIN_PORT    ?= 9090
MOCK_MINT_PORT    ?= 9444
APP_DB_PASSWORD   ?= meterwise
DEV_ADMIN_EMAIL   ?= admin@meterwise.local
# docker compose reads .env itself; export what make resolved so a command-line override
# (make up APP_API_PORT=18081) reaches compose too.
export METERWISE_PG_PORT APP_PG_PORT APP_API_PORT APP_PROXY_PORT APP_ADMIN_PORT MOCK_MINT_PORT APP_DB_PASSWORD

# The app stack is the compose profile "app"; infra-* and db-* touch only the shared `postgres` service.
STACK          = docker compose --profile app
STACK_SERVICES = appdb clerkmock migrate meterwised
PROJECT        = $(or $(COMPOSE_PROJECT_NAME),meterwise)
STACK_DB_URL   = postgres://meterwise:$(APP_DB_PASSWORD)@127.0.0.1:$(APP_PG_PORT)/meterwise?sslmode=disable
NATIVE_ENV     = env DATABASE_URL='$(DATABASE_URL)' APP_ENV=dev

.DEFAULT_GOAL := help
.PHONY: help setup infra-up infra-down infra-reset infra-logs infra-ps dev dev-stop dev-logs run \
        up down restart logs ps rebuild docker-build docker-migrate docker-bootstrap-admin docker-psql docker-shell \
        docker-demo-traffic docker-reset migrate migrate-status bootstrap-admin psql db-up db-down \
        build web gen sqlc test check fast lint fmt hooks worktree demo-traffic clean

help: ## Show this list
	@awk 'BEGIN {FS = ":.*## "} /^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5); next} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf '\nFirst time: make setup, then make up (Docker) or make infra-up dev (native). Guide: docs/runbooks/local-dev.md\n'

##@ Setup
setup: ## Check tools, download deps (go, tools, npm ci), install git hooks, create .env
	@deploy/dev/setup.sh

##@ Local (native Go + Vite, Postgres in Docker)
infra-up: ## Start Postgres 16 on 127.0.0.1:54329 (tmpfs: data is lost on stop)
	docker compose up -d --wait postgres
infra-down: ## Stop the shared Postgres (it is every worktree's test DB); its data is gone
	docker compose stop postgres
infra-reset: ## Recreate the shared Postgres empty
	docker compose rm -sf postgres
	$(MAKE) infra-up
infra-logs: ## Follow the shared Postgres logs
	docker compose logs -f --tail=100 postgres
infra-ps: ## Show the shared Postgres container
	docker compose ps postgres
dev: build ## Native: migrate, run meterwised (APP_ENV=dev) and the Vite dev server in the background
	@$(NATIVE_ENV) APP_API_PORT=$(APP_API_PORT) APP_PROXY_PORT=$(APP_PROXY_PORT) deploy/dev/native.sh start
dev-stop: ## Stop what make dev started
	@deploy/dev/native.sh stop
dev-logs: ## Follow the native dev logs
	@deploy/dev/native.sh logs
run: ## Foreground meterwised serve with APP_ENV=dev (no web dev server)
	$(NATIVE_ENV) go run ./cmd/meterwised serve

##@ Docker (full stack: Postgres, migrate, Clerk mock, meterwised; compose profile "app")
up: ## Build and start the stack, then print the URLs and how to sign in
	$(STACK) up -d --build $(STACK_SERVICES)
	@echo "waiting for http://127.0.0.1:$(APP_API_PORT)/healthz ..."
	@for i in $$(seq 1 90); do curl -fs http://127.0.0.1:$(APP_API_PORT)/healthz >/dev/null && break; sleep 1; done; \
	  curl -fs http://127.0.0.1:$(APP_API_PORT)/healthz >/dev/null || { echo "meterwised did not become healthy: make logs" >&2; exit 1; }
	@$(MAKE) --no-print-directory docker-bootstrap-admin EMAIL='$(DEV_ADMIN_EMAIL)' QUIET=1
	@printf '\n  Open   http://127.0.0.1:$(APP_API_PORT)\n  Sign in with the mock using %s (other emails are refused: not invited)\n  Proxy  http://127.0.0.1:$(APP_PROXY_PORT)    Admin  http://127.0.0.1:$(APP_ADMIN_PORT)/statusz    Postgres  127.0.0.1:$(APP_PG_PORT)\n  Next:  make logs | make docker-psql | make down\n\n' '$(DEV_ADMIN_EMAIL)'
down: ## Remove the app containers (database volume kept); the shared postgres is untouched
	$(STACK) stop $(STACK_SERVICES)
	$(STACK) rm -f $(STACK_SERVICES)
restart: ## Restart the Clerk twin and meterwised (reloads the twin's certificate)
	$(STACK) restart clerkmock meterwised
logs: ## Follow the stack logs (SERVICE=meterwised to pick one)
	$(STACK) logs -f --tail=100 $(or $(SERVICE),$(STACK_SERVICES))
ps: ## Show the stack containers
	$(STACK) ps -a $(STACK_SERVICES)
rebuild: ## Rebuild the images without cache and recreate the stack
	$(STACK) build --no-cache $(STACK_SERVICES)
	$(STACK) up -d --force-recreate $(STACK_SERVICES)
docker-build: ## Build the RELEASE image meterwise:local (needs CLERK_PUBLISHABLE_KEY=pk_...)
	@test -n "$(CLERK_PUBLISHABLE_KEY)" || { echo "docker-build: set CLERK_PUBLISHABLE_KEY (your Clerk publishable key, public) in .env or on the command line" >&2; exit 2; }
	docker build --target release -t meterwise:local --build-arg VITE_CLERK_PUBLISHABLE_KEY='$(CLERK_PUBLISHABLE_KEY)' \
	  --build-arg VERSION='$(VERSION)' --build-arg COMMIT='$(COMMIT)' .
docker-migrate: ## Apply migrations to the stack database
	$(STACK) run --rm migrate
docker-bootstrap-admin: ## First admin invitation in the stack: EMAIL=you@example.com
	@test -n "$(EMAIL)" || { echo "usage: make docker-bootstrap-admin EMAIL=you@example.com" >&2; exit 2; }
	@$(STACK) exec -T meterwised /app/dev-entrypoint.sh bootstrap-admin --email '$(EMAIL)' $(if $(QUIET),>/dev/null 2>&1 || echo "  (admin already exists or is invited)",)
docker-psql: ## psql into the stack database
	$(STACK) exec appdb psql -U meterwise -d meterwise
docker-shell: ## Shell in the meterwised container
	$(STACK) exec meterwised sh
docker-demo-traffic: ## demo-traffic against the stack database (ROUNDS=n WORKSPACE=<uuid>)
	DATABASE_URL='$(STACK_DB_URL)' go run ./cmd/demo-traffic -rounds $(or $(ROUNDS),5) $(if $(WORKSPACE),-workspace $(WORKSPACE))
docker-reset: ## DESTROY the stack's database volume and certificate; run make up after
	$(STACK) stop $(STACK_SERVICES)
	$(STACK) rm -f $(STACK_SERVICES)
	docker volume rm $(PROJECT)_appdb-data $(PROJECT)_ca

##@ Database (native)
migrate: ## Apply migrations to DATABASE_URL
	$(NATIVE_ENV) go run ./cmd/meterwised migrate
migrate-status: ## Show migration status for DATABASE_URL
	$(NATIVE_ENV) go run ./cmd/meterwised migrate status
bootstrap-admin: ## First admin invitation natively: EMAIL=you@example.com
	@test -n "$(EMAIL)" || { echo "usage: make bootstrap-admin EMAIL=you@example.com" >&2; exit 2; }
	$(NATIVE_ENV) go run ./cmd/meterwised bootstrap-admin --email '$(EMAIL)'
psql: ## psql into DATABASE_URL (falls back to the postgres container)
	@if command -v psql >/dev/null 2>&1; then psql '$(DATABASE_URL)'; else docker compose exec postgres psql -U meterwise -d meterwise; fi
db-up: ## Alias of infra-up
	docker compose up -d --wait postgres
db-down: ## Stop and remove the default compose services (shared postgres); not the app profile
	docker compose down

##@ Quality
test: ## Go and web tests through the gate
	scripts/run-checks.sh test
check: ## THE gate (CI runs exactly this)
	scripts/run-checks.sh
fast: ## Inner loop: fmt vet arch docs size sqlc
	scripts/run-checks.sh --fast
lint: ## golangci-lint through the gate
	scripts/run-checks.sh lint
fmt: ## gofmt -w
	gofmt -w .
sqlc: ## Regenerate sqlc code
	bash -c '. scripts/lib.sh && gotool sqlc generate'
gen: ## Regenerate the TS twin, openapi.json and docs/schema.md
	go run ./cmd/gen

build: ## Build bin/meterwised
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/meterwised ./cmd/meterwised
web: ## Release web build (needs VITE_CLERK_PUBLISHABLE_KEY)
	cd web && npm ci --no-audit --no-fund && npm run build:release

##@ Utilities
hooks: ## Install git hooks for this clone or worktree
	scripts/install-hooks.sh
# make worktree PLAN=05  -> ../meterwise-05 on branch wave/05
worktree: ## New worktree for a plan: PLAN=05
	@test -n "$(PLAN)" || { echo "usage: make worktree PLAN=05" >&2; exit 2; }
	git worktree add ../meterwise-$(PLAN) -b wave/$(PLAN)
	cd ../meterwise-$(PLAN) && scripts/install-hooks.sh

# Replays synthetic agent sessions through an in-process proxy with the ledger into DATABASE_URL
# (loopback fake upstream, no provider keys). ROUNDS=n sessions; WORKSPACE=<uuid> optional.
demo-traffic: ## Fill the ledger with demo sessions (native DATABASE_URL; ROUNDS=n WORKSPACE=<uuid>)
	$(NATIVE_ENV) go run ./cmd/demo-traffic -rounds $(or $(ROUNDS),5) $(if $(WORKSPACE),-workspace $(WORKSPACE))
clean: ## Remove bin, logs, web/dist, coverage and .dev
	rm -rf bin .logs web/dist web/coverage .dev
