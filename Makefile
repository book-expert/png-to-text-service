# PNG-to-Text-Service Makefile
# Follows strict Go coding standards with comprehensive tooling

.PHONY: all build test lint clean fmt vet check deps install help

# Default target
all: check build test

# Build the application
build:
	@echo "Building png-to-text-service..."
	@go build -o bin/png-to-text-service ./cmd/png-to-text-service

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy

# Format code
fmt:
	@echo "Formatting code..."
	@gofmt -w -s .

# Vet code
vet:
	@echo "Running go vet..."
	@go vet ./...

# Run comprehensive linting
lint:
	@echo "Running golangci-lint..."
	@gofmt -w -s .
	@go vet ./...
	@golangci-lint run --fix ./...
	@golangci-lint cache clean



# Run tests
test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...

# Run tests with coverage report
test-coverage: test
	@echo "Generating coverage report..."
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...

# Comprehensive quality check (lint + vet + test)
check: fmt vet lint test

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html

# Install the binary to $GOPATH/bin
install: build
	@echo "Installing to $GOPATH/bin..."
	@cp bin/png-to-text-service $(GOPATH)/bin/

# Development setup
dev-setup:
	@echo "Setting up development environment..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run the application with example configuration
run:
	@echo "Running png-to-text-service..."
	@./bin/png-to-text-service -config project.toml

# Generate Go module documentation
docs:
	@echo "Generating documentation..."
	@godoc -http=:6060 &
	@echo "Documentation server started at http://localhost:6060"

# Help target
help:
	@echo "Available targets:"
	@echo "  all         - Run check, build, and test"
	@echo "  build       - Build the application binary"
	@echo "  deps        - Install and tidy dependencies"
	@echo "  fmt         - Format Go code"
	@echo "  vet         - Run go vet"
	@echo "  lint        - Run golangci-lint"
	@echo "  test        - Run tests with race detection"
	@echo "  test-coverage - Run tests and generate coverage report"
	@echo "  bench       - Run benchmarks"
	@echo "  check       - Run comprehensive quality checks"
	@echo "  clean       - Clean build artifacts"
	@echo "  install     - Install binary to \$$GOPATH/bin"
	@echo "  dev-setup   - Set up development tools"
	@echo "  run         - Run the application"
	@echo "  docs        - Start documentation server"
	@echo "  help        - Show this help message"
