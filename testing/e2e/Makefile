# Nested Watchtower e2e engine. Parent make test does not recurse here.

GO ?= go
WORKERS ?= 1
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_CONFIG := $(CURDIR)/../../build/golangci-lint/golangci-lint.yaml
TAILWIND ?= tailwindcss
NODE_PATH ?= $(HOME)/.bun/install/global/node_modules

.PHONY: test run run-smoke list doctor lint vet fmt generate css clean serve

test: ## Engine unit tests (no Docker required)
	$(GO) test ./engine ./report ./watchtower ./registry ./assert ./run ./cmd ./store ./host ./stream ./control ./api ./web ./server -count=1

run-smoke: ## Unscoped run-once with Watchtower defaults (needs Docker)
	$(GO) run . run --workers 1 --generator file --file testdata/cases/smoke.yaml --keep

serve: ## Control plane (Postgres + Loki via compose, API + dashboard)
	$(GO) run . serve

run: ## Queue a sitting on the control plane. Hours. Shard it.
	$(GO) run . run --workers $(WORKERS)

list: ## Print product cardinality and factor table (no Docker)
	$(GO) run . list --dump-factors

doctor: ## DinD ping, no-egress, --help vs Model() coverage
	$(GO) run . doctor

lint: ## Lint the nested module
	$(GOLANGCI_LINT) run --config $(GOLANGCI_CONFIG) ./...

vet: ## go vet the nested module
	$(GO) vet ./...

generate: ## Regenerates templ and Tailwind CSS
	$(GO) run github.com/a-h/templ/cmd/templ@v0.3.1020 generate
	$(MAKE) css

css: ## Compile Tailwind from templ/Go class names into static/app.css
	NODE_PATH=$(NODE_PATH) $(TAILWIND) -i web/static/input.css -o web/static/app.css --minify

fmt: ## Format the nested module
	$(GO) run github.com/a-h/templ/cmd/templ@v0.3.1020 generate
	$(MAKE) css
	$(GOLANGCI_LINT) fmt --config $(GOLANGCI_CONFIG) ./...

clean: ## Remove e2e run artifacts
	rm -rf artifacts/*
