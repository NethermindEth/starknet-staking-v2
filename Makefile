.PHONY: validator signer docs-gen

DOCS_SRC ?= .
GIT_VERSION = $(shell git describe --tags | sed 's/^v//')
GO_LDFLAGS = -X github.com/NethermindEth/starknet-staking-v2/validator.Version=$(or $(VERSION),$(GIT_VERSION),dev)

validator:
	mkdir -p build
	go build -trimpath -ldflags "$(GO_LDFLAGS)" -o "./build/validator" "./cmd/validator/."

signer:
	mkdir -p build
	go build -trimpath -ldflags "$(GO_LDFLAGS)" -o "./build/signer" "./cmd/signer/."

clean-testcache: ## Clean Go test cache
	go clean -testcache

test:
	 go test ./...

test-race:
	go test -race ./...

test-cover: clean-testcache ## Run tests with coverage
	mkdir -p coverage
	go test -coverprofile=coverage/coverage.out -covermode=atomic ./...
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	open coverage/coverage.html

generate: ## Generate mocks
	mkdir -p mocks
	go generate ./...

lint: lint-diff
	go tool -modfile=golangci-lint.mod golangci-lint run

lint-diff:
	go tool -modfile=golangci-lint.mod golangci-lint run --config=./.golangci_diff.yaml --new-from-rev=origin/main

docs-gen:
	go run ./tools/docsgen -src $(DOCS_SRC) -out docs/docs
