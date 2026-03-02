# =============================================================================
# Botwallet CLI - Makefile
# =============================================================================
# Build and distribution commands for the CLI
# =============================================================================

# Version info
VERSION ?= 0.1.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build flags
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -s -w"

# Output directory
DIST := dist

# Binary name
BINARY := botwallet

.PHONY: all build clean test install deps lint fmt help

# Default target
all: build

# =============================================================================
# Development
# =============================================================================

## deps: Download dependencies
deps:
	go mod download
	go mod tidy

## build: Build for current platform
build: deps
	go build $(LDFLAGS) -o $(BINARY) .

## install: Install to GOPATH/bin
install: deps
	go install $(LDFLAGS) .

## run: Run the CLI (pass ARGS to run with arguments)
run: build
	./$(BINARY) $(ARGS)

## test: Run tests
test:
	go test -v ./...

## lint: Run linter
lint:
	golangci-lint run

## fmt: Format code
fmt:
	go fmt ./...
	gofmt -s -w .

# =============================================================================
# Cross Compilation
# =============================================================================

## build-all: Build for all platforms
build-all: clean build-linux build-darwin build-windows
	@echo "All builds complete!"
	@ls -la $(DIST)/

## build-linux: Build for Linux (amd64 and arm64)
build-linux: deps
	@mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-linux-arm64 .

## build-darwin: Build for macOS (amd64 and arm64)
build-darwin: deps
	@mkdir -p $(DIST)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-darwin-arm64 .

## build-windows: Build for Windows (amd64)
build-windows: deps
	@mkdir -p $(DIST)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-windows-amd64.exe .

# =============================================================================
# Release
# =============================================================================

## release: Create release archives
release: build-all
	@mkdir -p $(DIST)/release
	@cd $(DIST) && tar -czf release/$(BINARY)-$(VERSION)-linux-amd64.tar.gz $(BINARY)-linux-amd64
	@cd $(DIST) && tar -czf release/$(BINARY)-$(VERSION)-linux-arm64.tar.gz $(BINARY)-linux-arm64
	@cd $(DIST) && tar -czf release/$(BINARY)-$(VERSION)-darwin-amd64.tar.gz $(BINARY)-darwin-amd64
	@cd $(DIST) && tar -czf release/$(BINARY)-$(VERSION)-darwin-arm64.tar.gz $(BINARY)-darwin-arm64
	@cd $(DIST) && zip -q release/$(BINARY)-$(VERSION)-windows-amd64.zip $(BINARY)-windows-amd64.exe
	@echo "Release archives created in $(DIST)/release/"
	@ls -la $(DIST)/release/

## checksums: Generate checksums for releases
checksums: release
	@cd $(DIST)/release && sha256sum * > checksums.txt
	@echo "Checksums:"
	@cat $(DIST)/release/checksums.txt

# =============================================================================
# Cleanup
# =============================================================================

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf $(DIST)
	go clean

# =============================================================================
# Help
# =============================================================================

## help: Show this help message
help:
	@echo "Botwallet CLI - Build Commands"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/  /'



