.PHONY: validator signer

validator:
	mkdir -p build
	go build -ldflags "-X github.com/NethermindEth/starknet-staking-v2/validator.Version=$(shell git describe --tags)" -o "./build/validator" "./cmd/validator/."

signer:
	mkdir -p build
	go build -o "./build/signer" "./cmd/signer/."

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

lint:
	go tool golangci-lint run --fix
