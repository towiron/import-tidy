.PHONY: install-deps lint build
.SILENT:

# Install development dependencies
install-deps:
	@GOBIN=$(CURDIR)/temp/bin go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest



# Run linter
lint:
	@$(CURDIR)/temp/bin/golangci-lint run -c .golangci.yaml --path-prefix . --fix



# Build the binary
build:
	go build -o import-tidy
