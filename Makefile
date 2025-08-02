.PHONY: all build test clean run docker-build docker-push

# Variables
SERVICE_NAME := graph-service
DOCKER_REGISTRY := memql
DOCKER_IMAGE := $(DOCKER_REGISTRY)/$(SERVICE_NAME)
VERSION := $(shell git describe --tags --always --dirty)
GO_FLAGS := -ldflags="-X main.Version=$(VERSION)"

# Default target
all: test build

# Build the service
build:
	@echo "Building $(SERVICE_NAME)..."
	@go build $(GO_FLAGS) -o bin/$(SERVICE_NAME) cmd/service/main.go

# Run tests
test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out

# Run tests with coverage report
test-coverage: test
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run the service locally
run: build
	@echo "Running $(SERVICE_NAME)..."
	@./bin/$(SERVICE_NAME)

# Run with hot reload for development
dev:
	@echo "Running $(SERVICE_NAME) in development mode..."
	@air -c .air.toml

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/ coverage.out coverage.html

# Lint the code
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@goimports -w .

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest .

# Push Docker image
docker-push: docker-build
	@echo "Pushing Docker image..."
	@docker push $(DOCKER_IMAGE):$(VERSION)
	@docker push $(DOCKER_IMAGE):latest

# Run Neo4j locally for development
neo4j-local:
	@echo "Starting local Neo4j..."
	@docker run -d --name neo4j-dev \
		-p 7474:7474 -p 7687:7687 \
		-e NEO4J_AUTH=neo4j/password \
		-e NEO4J_PLUGINS='["apoc"]' \
		neo4j:5.0

# Stop local Neo4j
neo4j-stop:
	@echo "Stopping local Neo4j..."
	@docker stop neo4j-dev
	@docker rm neo4j-dev

# Generate mocks
mocks:
	@echo "Generating mocks..."
	@mockgen -source=internal/neo4j/client.go -destination=internal/mocks/neo4j_mock.go -package=mocks

# Run integration tests
integration-test:
	@echo "Running integration tests..."
	@go test -v -tags=integration ./...

# Check for security vulnerabilities
security:
	@echo "Checking for vulnerabilities..."
	@gosec ./...
	@go list -json -deps ./... | nancy sleuth

# Generate API documentation
docs:
	@echo "Generating API documentation..."
	@swag init -g cmd/service/main.go -o ./docs

# Install development tools
tools:
	@echo "Installing development tools..."
	@go install github.com/cosmtrek/air@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/golang/mock/mockgen@latest
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
	@go install github.com/sonatype-nexus-community/nancy@latest
	@go install github.com/swaggo/swag/cmd/swag@latest
	@go install golang.org/x/tools/cmd/goimports@latest