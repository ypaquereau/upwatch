.DEFAULT_GOAL := help
.PHONY: help build-bin test lint

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

build-bin: ## Build the binary into ./bin
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/upwatch ./cmd/upwatch

test: ## Run Go tests
	go test -race -cover ./...

lint: ## Run linters (extended as Terraform/Helm are added)
	go vet ./...
	golangci-lint run

# TODO (see SPEC section 8): dev, build, cluster, push-local, deploy, up, seed, grafana, down
