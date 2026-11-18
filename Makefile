.DEFAULT_GOAL := help

.PHONY: help all build install test race vet vuln fmt docs demo check acceptance clean

help: ## Show available targets (the default).
	@printf 'Usage: make <target>\n\n'
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z_-]+:.*## / { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

all: build ## Build bw.

build: ## Build bin/bw.
	go build -o bin/bw ./cmd/bw

install: ## Install bw into GOBIN (or Go's default bin directory).
	go install ./cmd/bw

test: ## Run all tests without using cached results.
	go test -count=1 ./...

race: ## Run all tests with the race detector and no cached results.
	go test -race -count=1 ./...

vet: ## Run Go's static checks.
	go vet ./...

vuln: ## Check for known vulnerabilities with the pinned Go tool.
	go tool govulncheck ./...

fmt: ## Format Go source and tests.
	gofmt -w cmd internal

docs: ## Regenerate README from the CLI help text.
	./scripts/docs.sh

demo: ## Run the two-worker demo in disposable local repositories.
	./examples/try-it.sh

check: acceptance ## Run the full acceptance suite.

acceptance: ## Run tests, race tests, vet, vulnerability checks and the demo.
	./scripts/acceptance.sh

clean: ## Remove the local build binary.
	rm -f bin/bw
