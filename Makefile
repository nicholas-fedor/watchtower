# Watchtower Makefile
# Comprehensive build, test, and deployment targets for the Watchtower project

# Variables
BINARY_NAME=watchtower
GO=go
DOCKER=docker
GORELEASER=goreleaser
GOLANGCI_LINT=golangci-lint
MOCKERY=mockery

# Default target
help: ## Show this help message
	@echo "Watchtower Project Makefile"
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# =============================================================================
# Development Targets
# =============================================================================

.PHONY: build test mocks lint vet run install

build: ## Build the application binary
	$(GO) build -o bin/$(BINARY_NAME) ./...

test: ## Run all tests
	$(GO) test -timeout 30s -v -coverprofile coverage.out -covermode atomic ./...

mocks: ## Generate mocks
	$(MOCKERY) --config build/mockery/mockery.yaml

lint: ## Run linter and fix issues
	$(GOLANGCI_LINT) run --fix --config build/golangci-lint/golangci-lint.yaml ./...

vet: ## Run Go vet
	$(GO) vet ./...

fmt: ## Run formatter
	$(GOLANGCI_LINT) fmt --config build/golangci-lint/golangci-lint.yaml ./...

run: ## Run the application
	$(GO) run ./...

install: ## Install the application
	$(GO) install ./...

# =============================================================================
# Dependency Management
# =============================================================================

.PHONY: mod-tidy mod-download

mod-tidy: ## Tidy and clean up Go modules
	$(GO) mod tidy

mod-download: ## Download Go module dependencies
	$(GO) mod download

# =============================================================================
# Documentation Targets
# =============================================================================

.PHONY: docs docs-setup docs-build docs-serve docs-activate docs-deactivate

docs: docs-setup docs-serve ## Build and serve documentation site for local development

docs-setup: ## Create virtual environment and install Mkdocs dependencies
	python3 -m venv watchtower-docs
	. watchtower-docs/bin/activate && pip install -r build/mkdocs/docs-requirements.txt

docs-gen: ## Generate service configuration documentation
	bash ./scripts/build-tplprev.sh

docs-build: ## Build Mkdocs documentation site
	. watchtower-docs/bin/activate && mkdocs build --config-file build/mkdocs/mkdocs.yaml

docs-serve: ## Serve Mkdocs documentation site locally
	. watchtower-docs/bin/activate && mkdocs serve --config-file build/mkdocs/mkdocs.yaml --livereload

docs-activate: ## Activate the watchtower-docs virtual environment for interactive work
	@echo "Run '. watchtower-docs/bin/activate' to activate the virtual environment."

docs-deactivate: ## Show instructions to deactivate the virtual environment
	@echo "Run 'deactivate' to exit the virtual environment."

# =============================================================================
# Release Targets
# =============================================================================

.PHONY: release

release: ## Create a new release using GoReleaser
	$(GORELEASER) release --clean

# =============================================================================
# Docker Targets
# =============================================================================

.PHONY: docker-build docker-run docker-push

docker-build: ## Build Docker image
	$(DOCKER) build -t $(BINARY_NAME) .

docker-run: ## Run Docker container
	$(DOCKER) run -p 8080:8080 $(BINARY_NAME)

# =============================================================================
# Utility Targets
# =============================================================================

.PHONY: clean

clean: ## Clean build artifacts
	rm -rf bin/
