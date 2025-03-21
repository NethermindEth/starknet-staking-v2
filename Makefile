
# TODO: should we use protobuf when generating mocks ?
# generate-buf: ## Generate protobuf files
# 	@buf generate

generate: ## Generate mocks and code
	mkdir -p mocks
	go generate ./...