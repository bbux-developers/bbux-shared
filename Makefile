# bbux-shared — shared @bbux/* packages + an examples playground.
# The packages are linked locally via the pnpm workspace, so edits under
# packages/ts/* show up live in the examples app.

.DEFAULT_GOAL := help

.PHONY: help install examples dev build clean

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
