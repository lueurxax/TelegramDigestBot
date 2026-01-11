# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Run Commands

```bash
# Build the binary
go build -o digest-bot ./cmd/digest-bot/main.go

# Run individual modes (requires .env or environment variables)
go run ./cmd/digest-bot/main.go --mode=bot      # Control bot (admin commands)
go run ./cmd/digest-bot/main.go --mode=reader   # MTProto message ingester
go run ./cmd/digest-bot/main.go --mode=worker   # LLM processing pipeline
go run ./cmd/digest-bot/main.go --mode=digest   # Digest scheduler
go run ./cmd/digest-bot/main.go --mode=digest --once  # Run digest once and exit

# Docker Compose
docker-compose -f deploy/compose/docker-compose.yml up -d
docker-compose -f deploy/compose/docker-compose.yml build

# Generate SQL code from queries (requires sqlc installed)
sqlc generate
```

## Architecture Overview

This is a Telegram digest bot with four distinct operational modes, all sharing the same binary:

### Modes (cmd/digest-bot/main.go)
- **bot**: Telegram Bot API control interface for admins (add/remove channels, configure settings)
- **reader**: MTProto client (gotd/td) that ingests messages from tracked channels as a user account
- **worker**: Processing pipeline that filters, deduplicates, and scores messages via LLM
- **digest**: Scheduler that posts periodic digests with semantic clustering to target channel

### Data Flow
```
Channels → Reader (MTProto) → raw_messages table → Worker (LLM) → items table → Digest → Telegram
```

### Key Internal Packages
- `internal/config`: Environment-based configuration via caarlos0/env
- `internal/db`: PostgreSQL with pgx, goose migrations, sqlc-generated queries
- `internal/telegramreader`: MTProto client handling Flood Waits, invite links, media downloads
- `internal/pipeline`: Batched LLM processing with filtering and deduplication
- `internal/digest`: Leader-elected scheduler with semantic clustering (pgvector)
- `internal/llm`: OpenAI client with embedding and batch processing support
- `internal/telegrambot`: Admin bot with settings management via database
- `internal/dedup`: Strict and semantic deduplication using cosine similarity
- `internal/filters`: Keyword and ad filtering

### Database
- Uses pgvector for semantic embeddings (1536 dimensions)
- Migrations in `migrations/` directory (goose format)
- SQL queries defined in `internal/db/queries.sql`, generated to `internal/db/sqlc/`
- Advisory locks for leader election and migration coordination

### Configuration
All configuration via environment variables (see `.env.example`). Settings can also be overridden via bot commands which store to database (e.g., `/relevance`, `/window`, `/model`).
