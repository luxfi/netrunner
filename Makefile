# Makefile for Lux Netrunner

# Variables
BINARY_NAME := netrunner
VERSION := $(shell git describe --tags --always --dirty="-dev" 2>/dev/null || echo "unknown")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date +%FT%T%z)
BUILD_DIR := build

# Go build flags
GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)
CGO_CFLAGS := -O -D__BLST_PORTABLE__
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

# Default target
.PHONY: all
all: build

# Build the netrunner binary
.PHONY: build
build:
	@echo "Building Netrunner..."
	@mkdir -p $(BUILD_DIR)
	CGO_CFLAGS="$(CGO_CFLAGS)" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) .

# Build using the build script
.PHONY: build-script
build-script:
	@echo "Building Netrunner using script..."
	@./scripts/build.sh

# Build release version
.PHONY: build-release
build-release:
	@echo "Building Netrunner release..."
	@./scripts/build.release.sh

# Install netrunner to system
.PHONY: install
install: build
	@echo "Installing netrunner..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "Successfully installed to /usr/local/bin/$(BINARY_NAME)"

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run tests with race detector
.PHONY: test-race
test-race:
	@echo "Running tests with race detector..."
	go test -race -v ./...

# Run benchmarks
.PHONY: bench
bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run linter
.PHONY: lint
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

# Generate mocks
.PHONY: mocks
mocks:
	@echo "Generating mocks..."
	@if command -v mockgen >/dev/null 2>&1; then \
		go generate ./...; \
	else \
		echo "mockgen not installed. Install with: go install github.com/golang/mock/mockgen@latest"; \
		exit 1; \
	fi

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)/
	@rm -f coverage.out coverage.html
	@rm -rf dist/
	@echo "Clean complete"

# Run security checks
.PHONY: security
security:
	@echo "Running security checks..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "gosec not installed. Install with: go install github.com/securego/gosec/v2/cmd/gosec@latest"; \
		exit 1; \
	fi

# Run static analysis
.PHONY: staticcheck
staticcheck:
	@echo "Running static analysis..."
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not installed. Install with: go install honnef.co/go/tools/cmd/staticcheck@latest"; \
		exit 1; \
	fi

# Update dependencies
.PHONY: deps
deps:
	@echo "Updating dependencies..."
	go mod download
	go mod tidy

# Verify dependencies
.PHONY: verify
verify:
	@echo "Verifying dependencies..."
	go mod verify

# Run all checks (fmt, lint, test)
.PHONY: check
check: fmt lint test

# Build for multiple platforms
.PHONY: build-all
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	# Linux AMD64
	GOOS=linux GOARCH=amd64 CGO_CFLAGS="$(CGO_CFLAGS)" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 .
	# Linux ARM64
	GOOS=linux GOARCH=arm64 CGO_CFLAGS="$(CGO_CFLAGS)" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 .
	# Darwin AMD64
	GOOS=darwin GOARCH=amd64 CGO_CFLAGS="$(CGO_CFLAGS)" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 .
	# Darwin ARM64
	GOOS=darwin GOARCH=arm64 CGO_CFLAGS="$(CGO_CFLAGS)" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 .
	# Windows AMD64
	GOOS=windows GOARCH=amd64 CGO_CFLAGS="$(CGO_CFLAGS)" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe .
	@echo "Multi-platform build complete"

# Run netrunner
.PHONY: run
run: build
	@echo "Running Netrunner..."
	./$(BUILD_DIR)/$(BINARY_NAME)

# Display version information
.PHONY: version
version:
	@echo "Lux Netrunner"
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "Go Version: $(shell go version)"
	@echo "OS/Arch: $(GOOS)/$(GOARCH)"

# Generate release artifacts
.PHONY: release
release: clean build-all
	@echo "Generating release artifacts..."
	@mkdir -p dist
	@cp $(BUILD_DIR)/* dist/
	@echo "Release artifacts in dist/"

# Help message
.PHONY: help
help:
	@echo "Lux Netrunner Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all          - Build the netrunner binary (default)"
	@echo "  build        - Build the netrunner binary"
	@echo "  build-script - Build using the build script"
	@echo "  build-release - Build release version"
	@echo "  install      - Build and install netrunner to /usr/local/bin"
	@echo "  test         - Run tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  test-race    - Run tests with race detector"
	@echo "  bench        - Run benchmarks"
	@echo "  fmt          - Format code"
	@echo "  lint         - Run linter"
	@echo "  mocks        - Generate mocks"
	@echo "  clean        - Clean build artifacts"
	@echo "  security     - Run security checks"
	@echo "  staticcheck  - Run static analysis"
	@echo "  deps         - Update dependencies"
	@echo "  verify       - Verify dependencies"
	@echo "  check        - Run fmt, lint, and test"
	@echo "  build-all    - Build for multiple platforms"
	@echo "  run          - Build and run netrunner"
	@echo "  version      - Display version information"
	@echo "  release      - Generate release artifacts"
	@echo "  help         - Display this help message"