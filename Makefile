.PHONY: help test test-unit test-worker test-coverage test-all clean db-up db-down db-reset lint mocks

# Default target
help:
	@echo "Available targets:"
	@echo "  make test              - Run all unit tests"
	@echo "  make test-unit         - Run unit tests with verbose output"
	@echo "  make test-coverage     - Run tests with coverage report"
	@echo "  make test-worker       - Run worker tests with race detection"
	@echo "  make test-all          - Run all database-free tests with race detection"
	@echo "  make db-up             - Start PostgreSQL container"
	@echo "  make db-down           - Stop PostgreSQL container"
	@echo "  make db-reset          - Reset database (down + up)"
	@echo "  make lint              - Run linters"
	@echo "  make mocks             - Generate mocks using mockery"
	@echo "  make clean             - Clean test artifacts"

# Unit tests (fast, no external dependencies)
test:
	@echo "Running unit tests..."
	@go test ./... -short -v

test-unit:
	@echo "Running unit tests with verbose output..."
	@go test ./... -short -v

# Test coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test ./... -coverprofile=coverage.out -covermode=atomic
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@go tool cover -func=coverage.out | grep total

# Worker tests
test-worker:
	@echo "Running worker tests with race detection..."
	@go test -race -count=1 -v ./internal/worker/...

# Run all database-free tests
test-all:
	@echo "Running all tests with race detection..."
	@go test ./... -short -race -count=1

# Database management
db-up:
	@echo "Starting PostgreSQL..."
	@docker-compose up -d db
	@echo "Waiting for database to be ready..."
	@sleep 3
	@docker-compose exec -T db pg_isready -U postgres || (echo "Database not ready" && exit 1)
	@echo "Database is ready!"

db-down:
	@echo "Stopping PostgreSQL..."
	@docker-compose down

db-reset: db-down db-up
	@echo "Database reset complete"

# Linting
lint:
	@echo "Running linters..."
	@go fmt ./...
	@go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

# Generate mocks
mocks:
	@echo "Generating mocks..."
	@if command -v mockery >/dev/null 2>&1; then \
		mockery; \
	else \
		echo "mockery not installed, installing..."; \
		go install github.com/vektra/mockery/v2@latest; \
		$(HOME)/go/bin/mockery; \
	fi
	@echo "Mocks generated successfully"

# Clean artifacts
clean:
	@echo "Cleaning test artifacts..."
	@rm -f coverage.out coverage.html
	@go clean -testcache
	@echo "Clean complete"
