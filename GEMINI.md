# Gemini Context: Telegram Noise-Reduction Digest Bot

## Project Overview
This project is a modular Telegram automation system written in Go. It ingests messages from tracked channels (using MTProto), filters them, and generates summarized digests using LLMs.

### Key Features
*   **MTProto Reader**: Ingests messages as a real user.
*   **Processing Pipeline**: Deduplication (pgvector), relevance scoring, summarization.
*   **LLM Integration**: Multi-provider routing with fallback chains.
*   **Architecture**: Microservices-like components (Control Bot, Reader, Worker, Scheduler) interacting via PostgreSQL.
*   **Research Dashboard**: Web UI for archive search, claims, clusters, and analytics.

## Architecture & Components
The system is divided into several Go services, typically run via Docker Compose or individually for development.

*   **Control Bot (`cmd/digest-bot --mode=bot`)**: Admin interface for managing channels and settings.
*   **Reader Service (`cmd/digest-bot --mode=reader`)**: Connects to Telegram via MTProto to fetch messages.
*   **Processor Worker (`cmd/digest-bot --mode=worker`)**: Background worker that processes messages (embeddings, filtering, summarization).
*   **Digest Scheduler (`cmd/digest-bot --mode=digest`)**: Manages the schedule for publishing digests.

### Directory Structure
*   `cmd/`: Application entry points.
    *   `digest-bot/`: Main unified binary for all modes.
    *   `crawler/`: Separate crawler component.
*   `internal/`: Private application code.
    *   `app/`: Application lifecycle management.
    *   `bot/`: Control bot implementation.
    *   `ingest/`: MTProto reader logic.
    *   `process/`: Message processing pipeline.
    *   `storage/`: Database interaction (SQLC generated code in `sqlc/`).
*   `migrations/`: SQL database migrations (Goose).
*   `deploy/`: Deployment configurations (Docker Compose, K8s).

## Development Workflow

### Prerequisites
*   Go 1.25.6+
*   Docker & Docker Compose
*   PostgreSQL with `pgvector` extension

### Key Commands (Makefile)
*   **Build**: `make build` (Outputs to `bin/telegram-digest-bot`)
*   **Test**: `make test` (Runs `go test ./...` with coverage if available)
*   **Lint**: `make lint` (Uses `golangci-lint`)
*   **Generate SQL**: `make generate` (Runs `sqlc generate`)
*   **Migrations**: `make migrate-up` / `make migrate-down`

### Running Locally
You can run individual components using the `go run` commands defined in the Makefile:
*   `make run-bot`
*   `make run-reader`
*   `make run-worker`
*   `make run-digest`

## Authoritative Rules

Follow the repository guidelines in [CLAUDE.md](CLAUDE.md) (linting rules, SQLC patterns, and project conventions).

### Database
*   Uses **PostgreSQL** as the primary store.
*   Uses **pgvector** for vector similarity search (deduplication).
*   SQL queries are defined in `internal/storage/queries.sql` and compiled to Go code using **sqlc**.

## Configuration
*   Configuration is handled via environment variables (see `.env.example`).
*   Key variables include Telegram API credentials (`TG_APP_ID`, `TG_APP_HASH`), Bot Token (`TG_BOT_TOKEN`), and LLM keys (`OPENAI_API_KEY`, etc.).
