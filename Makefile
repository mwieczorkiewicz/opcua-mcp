# Makefile for OPC-UA MCP Server

# Variables
BINARY_NAME=opcua-mcp
BUILD_DIR=build
DOCKER_IMAGE=opcua-mcp
DOCKER_TAG=latest

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt

# Build flags
LDFLAGS=-ldflags "-X main.version=$(shell git describe --tags --always --dirty) -X main.buildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)"

.PHONY: all build clean test test-integration deps fmt lint docker-build docker-run compose-build compose-up-server compose-up compose-down connector-url help

# Default target
all: clean deps fmt lint test build

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/opcua-mcp.go

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/opcua-mcp.go
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/opcua-mcp.go
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/opcua-mcp.go
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/opcua-mcp.go

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -rf search_index

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Run the integration test suite (requires a local Docker daemon - starts a
# real Microsoft OPC-UA test server via testcontainers-go). Excluded from
# `test`/`test-coverage` via the `integration` build tag so the default
# suite stays Docker-free and fast (requirements.md NFR-4.2).
test-integration:
	@echo "Running integration tests (requires Docker)..."
	$(GOTEST) -tags=integration -v ./...

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) ./...

# Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found, skipping linting"; \
	fi

# Run the application in stdio mode
run-stdio:
	@echo "Running in stdio mode..."
	$(BUILD_DIR)/$(BINARY_NAME)

# Run the application in HTTP mode
run-http:
	@echo "Running in HTTP mode..."
	SERVER_TRANSPORT=http $(BUILD_DIR)/$(BINARY_NAME)

# Run with custom OPC-UA server
run-with-server:
	@echo "Running with custom OPC-UA server..."
	SERVER_TRANSPORT=http \
	OPCUA_ENDPOINT=opc.tcp://localhost:4840 \
	OPCUA_AUTH_MODE=anonymous \
	$(BUILD_DIR)/$(BINARY_NAME)

# Run with username authentication
run-with-auth:
	@echo "Running with username authentication..."
	SERVER_TRANSPORT=http \
	OPCUA_ENDPOINT=opc.tcp://localhost:4840 \
	OPCUA_AUTH_MODE=username \
	OPCUA_USERNAME=admin \
	OPCUA_PASSWORD=secret \
	$(BUILD_DIR)/$(BINARY_NAME)

# Docker build
docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

# Docker run
docker-run:
	@echo "Running Docker container..."
	docker run -p 8080:8080 \
		-e SERVER_TRANSPORT=http \
		-e OPCUA_ENDPOINT=opc.tcp://host.docker.internal:4840 \
		-e OPCUA_AUTH_MODE=anonymous \
		$(DOCKER_IMAGE):$(DOCKER_TAG)

# Docker run with volume for search index
docker-run-with-volume:
	@echo "Running Docker container with volume..."
	docker run -p 8080:8080 \
		-v $(PWD)/search_index:/app/search_index \
		-e SERVER_TRANSPORT=http \
		-e OPCUA_ENDPOINT=opc.tcp://host.docker.internal:4840 \
		-e OPCUA_AUTH_MODE=anonymous \
		$(DOCKER_IMAGE):$(DOCKER_TAG)

# Install the binary to GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME)..."
	$(GOBUILD) $(LDFLAGS) -o $(GOPATH)/bin/$(BINARY_NAME) ./cmd/opcua-mcp.go

# Generate mocks (requires mockgen)
generate-mocks:
	@echo "Generating mocks..."
	@if command -v mockgen >/dev/null 2>&1; then \
		mockgen -source=internal/opcua/client.go -destination=internal/opcua/mocks/client_mock.go; \
		mockgen -source=internal/opcua/discovery.go -destination=internal/opcua/mocks/discovery_mock.go; \
	else \
		echo "mockgen not found, skipping mock generation"; \
	fi

# Benchmark tests
benchmark:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Security scan (requires gosec)
security-scan:
	@echo "Running security scan..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "gosec not found, skipping security scan"; \
	fi

# Check for vulnerabilities (requires govulncheck)
vuln-check:
	@echo "Checking for vulnerabilities..."
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "govulncheck not found, skipping vulnerability check"; \
	fi

# Development setup
dev-setup:
	@echo "Setting up development environment..."
	$(GOMOD) download
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v1.54.2; \
	fi
	@if ! command -v mockgen >/dev/null 2>&1; then \
		echo "Installing mockgen..."; \
		$(GOGET) github.com/golang/mock/mockgen@latest; \
	fi
	@if ! command -v gosec >/dev/null 2>&1; then \
		echo "Installing gosec..."; \
		$(GOGET) github.com/securecodewarrior/gosec/v2/cmd/gosec@latest; \
	fi

# Pull the docker-compose images (opcua-mcp now runs the prebuilt ghcr.io
# image pinned in docker-compose.yml, rather than building locally)
compose-build:
	@echo "Pulling docker-compose images..."
	docker compose pull

# Start the Microsoft OPC UA test server standalone (detached), for manual dev use
compose-up-server:
	@echo "Starting Microsoft OPC UA test server via docker-compose..."
	docker compose up -d opcua-server
	@echo "OPC UA Test Server started on opc.tcp://localhost:4840"
	@echo "Use 'make compose-down' to stop it"

# Start the full stack detached: OPC UA test server, opcua-mcp (HTTP mode), and the
# cloudflared quick-tunnel that gives it a public HTTPS URL. Follow with 'make connector-url'.
compose-up:
	@echo "Starting full stack (opcua-server, opcua-mcp, cloudflared tunnel)..."
	docker compose up -d
	@echo "Stack started. Run 'make connector-url' once cloudflared has connected."

# Stop and remove docker-compose services
compose-down:
	@echo "Stopping docker-compose services..."
	docker compose down

# Print the public HTTPS URL to paste into Claude's custom connector UI
# (Settings > Connectors > Add custom connector). Requires 'make compose-up' first;
# the tunnel URL is ephemeral and regenerates on every restart of the cloudflared service.
connector-url:
	@url=""; \
	for i in $$(seq 1 30); do \
		url=$$(docker compose logs cloudflared 2>/dev/null | grep -o 'https://[a-zA-Z0-9.-]*\.trycloudflare\.com' | tail -1); \
		if [ -n "$$url" ]; then break; fi; \
		sleep 1; \
	done; \
	if [ -z "$$url" ]; then \
		echo "No tunnel URL found yet - is 'make compose-up' running? Check 'docker compose logs cloudflared'."; \
		exit 1; \
	fi; \
	echo "Custom connector URL: $$url/mcp"

# Show help
help:
	@echo "Available targets:"
	@echo "  all              - Clean, deps, fmt, lint, test, build"
	@echo "  build            - Build the application"
	@echo "  build-all        - Build for multiple platforms"
	@echo "  clean            - Clean build artifacts"
	@echo "  test             - Run tests"
	@echo "  test-coverage    - Run tests with coverage"
	@echo "  test-integration - Run integration tests against a real OPC-UA test server (requires Docker)"
	@echo "  deps             - Download dependencies"
	@echo "  fmt              - Format code"
	@echo "  lint             - Run linter"
	@echo "  run-stdio        - Run in stdio mode"
	@echo "  run-http         - Run in HTTP mode"
	@echo "  run-with-server  - Run with custom OPC-UA server"
	@echo "  run-with-auth    - Run with username authentication"
	@echo "  docker-build     - Build Docker image"
	@echo "  docker-run       - Run Docker container"
	@echo "  docker-run-with-volume - Run Docker container with volume"
	@echo "  compose-build    - Pull docker-compose images (opcua-mcp runs the prebuilt ghcr.io image)"
	@echo "  compose-up-server - Start Microsoft OPC UA test server via docker-compose (detached)"
	@echo "  compose-up       - Start full stack: test server, opcua-mcp (HTTP), cloudflared tunnel"
	@echo "  compose-down     - Stop and remove docker-compose services"
	@echo "  connector-url    - Print the public HTTPS URL for Claude's custom connector UI"
	@echo "  install          - Install binary to GOPATH/bin"
	@echo "  generate-mocks   - Generate mocks"
	@echo "  benchmark        - Run benchmarks"
	@echo "  security-scan    - Run security scan"
	@echo "  vuln-check       - Check for vulnerabilities"
	@echo "  dev-setup        - Setup development environment"
	@echo "  help             - Show this help"
