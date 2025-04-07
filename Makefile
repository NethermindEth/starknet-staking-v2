
validator: ## compile
	mkdir -p build
	go build -o build/validator

clean-testcache: ## Clean Go test cache
	go clean -testcache

test:
	 go test ./...

test-cover: clean-testcache ## Run tests with coverage
	mkdir -p coverage
	go test -coverprofile=coverage/coverage.out -covermode=atomic .
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	open coverage/coverage.html

# Need to move mockgen interfaces to their own pkg
generate: ## Generate mocks
	mkdir -p mocks
	go generate ./...
