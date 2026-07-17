.PHONY: install-deps lint test build
.SILENT:

# Install development dependencies
install-deps:
	@GOBIN=$(CURDIR)/temp/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

# Run linter
lint:
	@$(CURDIR)/temp/bin/golangci-lint run -c .golangci.yaml --path-prefix . --fix

# Run tests
test:
	go test ./...

# Build the binary
build:
	go build -o import-tidy
