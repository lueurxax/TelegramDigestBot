.PHONY: build test lint lint-fix clean

# Build the application
build:
	go build -o bin/telegram-digest-bot ./cmd/digest-bot

# Run all tests
test:
	@if go tool covdata >/dev/null 2>&1; then \
		go test -race -cover ./...; \
	else \
		echo "go tool covdata not available; running tests without -cover"; \
		go test -race ./...; \
	fi

# Run linter
lint:
	~/go/bin/golangci-lint run ./...

# Run linter and fix issues
lint-fix:
	~/go/bin/golangci-lint run --fix ./...

# Generate SQLC code
generate:
	sqlc generate

# Run database migrations
migrate-up:
	goose -dir migrations postgres "$(POSTGRES_DSN)" up

migrate-down:
	goose -dir migrations postgres "$(POSTGRES_DSN)" down

# Clean build artifacts
clean:
	rm -rf bin/

# Install development tools
tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest

# Run the bot locally
run-bot:
	go run ./cmd/digest-bot --mode=bot

run-reader:
	go run ./cmd/digest-bot --mode=reader

run-worker:
	go run ./cmd/digest-bot --mode=worker

run-digest:
	go run ./cmd/digest-bot --mode=digest
