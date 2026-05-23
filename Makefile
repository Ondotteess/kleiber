# Kleiber — top-level developer Makefile.
#
# Targets are deliberately thin wrappers around the Go toolchain so that
# CI (.github/workflows/ci.yml) and local checks (scripts/check.{sh,ps1})
# behave identically.

GO          ?= go
PKG         := github.com/Ondotteess/kleiber
BIN_DIR     := bin
BINARY_NAME := kleiber

ifeq ($(OS),Windows_NT)
    BINARY_EXT := .exe
else
    BINARY_EXT :=
endif

BINARY := $(BIN_DIR)/$(BINARY_NAME)$(BINARY_EXT)

.PHONY: all help build run test test-race test-integration coverage \
        vet fmt lint tidy clean tools-check

all: build

help: ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the kleiber binary into ./bin
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -o $(BINARY) ./cmd/kleiber

run: build ## Build and run kleiber
	$(BINARY)

test: ## Run unit tests
	$(GO) test ./...

test-race: ## Run tests with the race detector
	$(GO) test -race ./...

test-integration: ## Run integration-tagged tests
	$(GO) test -tags=integration ./...

coverage: ## Produce an HTML coverage report at coverage.html
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report written to coverage.html"

vet: ## Run go vet
	$(GO) vet ./...

fmt: ## Run gofmt -s -w across the repo
	$(GO) fmt ./...

lint: vet ## Run gofmt check, staticcheck, golangci-lint (skip silently if missing)
	@drift=$$(gofmt -s -l . 2>/dev/null); \
	if [ -n "$$drift" ]; then \
	    echo "gofmt drift in:"; echo "$$drift"; exit 1; \
	fi
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed; skipping"
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; skipping"

tidy: ## Run go mod tidy (use sparingly — see docs/agents/forbidden-actions.md §10)
	$(GO) mod tidy

clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html

tools-check: ## Verify the Go toolchain is present
	@$(GO) version
	@command -v gofmt >/dev/null 2>&1 || (echo "gofmt missing" && exit 1)
	@echo "Toolchain OK"
