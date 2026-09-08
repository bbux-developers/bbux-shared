# bbux-shared — shared @bbux/* packages + an examples playground.
# The packages are linked locally via the pnpm workspace, so edits under
# packages/ts/* show up live in the examples app.

.DEFAULT_GOAL := help

.PHONY: help install examples dev build clean package

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

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
