# Nested Watchtower e2e engine. Parent make test does not recurse here.

GO ?= go
WORKERS ?= 1
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_CONFIG := $(CURDIR)/../../build/golangci-lint/golangci-lint.yaml

.PHONY: test run run-smoke list doctor lint vet fmt clean

test: ## Engine unit tests (no Docker required)
	$(GO) test ./engine ./report ./watchtower ./registry ./assert ./run ./cmd -count=1

run-smoke: ## Unscoped run-once with Watchtower defaults (needs Docker)
	$(GO) run . run --workers 1 --generator file --file testdata/cases/smoke.yaml --keep

run: ## Full cartesian stream. Hours. Shard it.
	$(GO) run . run --workers $(WORKERS)

list: ## Print product cardinality and factor table (no Docker)
	$(GO) run . list --dump-factors

doctor: ## DinD ping, no-egress, --help vs Model() coverage
	$(GO) run . doctor

lint: ## Lint the nested module
	$(GOLANGCI_LINT) run --config $(GOLANGCI_CONFIG) ./...

vet: ## go vet the nested module
	$(GO) vet ./...

fmt: ## Format the nested module
	$(GOLANGCI_LINT) fmt --config $(GOLANGCI_CONFIG) ./...

clean: ## Remove e2e run artifacts
	rm -rf artifacts/*
