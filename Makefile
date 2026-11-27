# bbux-shared — shared @bbux/* packages + an examples playground.
# The packages are linked locally via the pnpm workspace, so edits under
# packages/ts/* show up live in the examples app.

.DEFAULT_GOAL := help

.PHONY: help install examples dev build clean package \
        test-go test-go-integration lint-go tidy-go

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

install: ## Install the workspace (packages + examples)
	pnpm install

examples: install ## Run the examples playground at http://localhost:5199
	pnpm --filter @bbux/examples dev

dev: examples ## Alias for `make examples`

build: install ## Production-build the examples app (verifies everything compiles)
	pnpm --filter @bbux/examples build

clean: ## Remove all node_modules + build output
	rm -rf node_modules packages/ts/*/node_modules examples/node_modules examples/dist
	@find . -name .DS_Store -not -path './.git/*' -delete 2>/dev/null || true
	@echo "cleaned — \`make install\` regenerates all of it"

# Uses `git archive`, so the zip holds EXACTLY the committed tracked files — no
# node_modules, no build output, and nothing ignored can leak in whatever is
# lying around the working tree. Mirrors `make package` in the bbux repo.
PACKAGE_NAME ?= bbux-shared
package: ## Zip the repo (tracked files only) to send someone
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "⚠ working tree is dirty — the zip contains the last COMMIT, not your"; \
		echo "  uncommitted changes. Commit first if that matters."; \
	fi
	@rm -f $(PACKAGE_NAME).zip
	@git archive --format=zip --prefix=$(PACKAGE_NAME)/ -o $(PACKAGE_NAME).zip HEAD
	@echo "→ $(PACKAGE_NAME).zip  ($$(du -h $(PACKAGE_NAME).zip | cut -f1), $$(git ls-files | wc -l | tr -d ' ') files)"

# ── Go modules ──────────────────────────────────────────────────
# Every directory under packages/go is its OWN Go module (consumers pin each
# by commit pseudo-version), so `go test ./...` at the root sees nothing — these
# targets loop over the modules. MOD=<name> narrows to one (e.g. MOD=migrate);
# RUN=<regex> narrows the tests.
GO_MODULES := $(if $(MOD),packages/go/$(MOD),$(patsubst %/go.mod,%,$(wildcard packages/go/*/go.mod)))

define each_go_module
	@status=0; for m in $(GO_MODULES); do \
		echo "── $$m"; (cd $$m && $(1)) || status=1; \
	done; exit $$status
endef

test-go: ## Go tests for every module (DB tests skip; MOD=<name> RUN=<regex> V=1)
	$(call each_go_module,go test -count=1 $(if $(V),-v) $(if $(RUN),-run '$(RUN)') ./...)

lint-go: ## go vet + gofmt check for every Go module
	$(call each_go_module,go vet ./... && test -z "$$(gofmt -l .)" || { gofmt -l .; false; })

tidy-go: ## go mod tidy every Go module (fetches/prunes deps)
	$(call each_go_module,go mod tidy)

# Throwaway Postgres for the DB-backed tests (migrate, seed). Modelled on bbux's
# `test-api-integration`. Port 5499 stays clear of every other repo's host ports.
TEST_PG_PORT ?= 5499
TEST_PG_NAME ?= bbux-shared-test-db
TEST_PG_URL  := postgres://postgres:postgres@localhost:$(TEST_PG_PORT)/shared_test?sslmode=disable

test-go-integration: ## Go tests incl. DB tests vs a throwaway Postgres on :5499
	@docker rm -f $(TEST_PG_NAME) >/dev/null 2>&1 || true
	@docker run -d --name $(TEST_PG_NAME) \
		-e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=shared_test \
		-p $(TEST_PG_PORT):5432 postgres:16-alpine >/dev/null
	@echo "waiting for test db…"
	@# The entrypoint runs a TEMPORARY server for initdb then restarts the real
	@# one, so require several consecutive real-query successes.
	@ok=0; while [ $$ok -lt 5 ]; do \
		if docker exec $(TEST_PG_NAME) psql -U postgres -d shared_test -c 'SELECT 1' >/dev/null 2>&1; then \
			ok=$$((ok+1)); else ok=0; fi; sleep 1; \
	done
	@status=0; for m in $(GO_MODULES); do \
		echo "── $$m"; \
		(cd $$m && TEST_DATABASE_URL="$(TEST_PG_URL)" go test -count=1 -p 1 $(if $(V),-v) $(if $(RUN),-run '$(RUN)') ./...) || status=1; \
	done; docker rm -f $(TEST_PG_NAME) >/dev/null 2>&1 || true; exit $$status
