# Architecture Overview

This document describes the high-level architecture, package structure, and dependency patterns of the Telegram Digest Bot.

## Package Structure

The codebase organizes packages under `internal/` into clear domains with explicit responsibilities:

```
internal/
  app/           # Runtime wiring and service startup
  bot/           # Admin bot and operator-facing commands
  core/          # Shared domain services
    domain/      # Shared domain types
    links/       # Link extraction and resolution
    llm/         # LLM client for summarization and scoring
  ingest/        # Telegram message intake
    reader/      # MTProto reader for channel messages
  output/        # Digest generation and publishing
    digest/      # Clustering, selection, rendering, scheduling
  platform/      # Infrastructure concerns
    config/      # Configuration loading
    htmlutils/   # HTML parsing utilities
    observability/ # Health checks and metrics
  process/       # Message processing pipeline
    dedup/       # Deduplication logic
    filters/     # Content filtering
    pipeline/    # Main processing pipeline
  storage/       # Database layer
    sqlc/        # Generated query code
    migrations/  # Database schema migrations

cmd/
  digest-bot/    # Single runtime entrypoint with --mode flag
  tools/         # Utility binaries
    eval/        # Evaluation tooling
    labels/      # Labeling tooling
```

## Runtime Flows

The system processes messages through four main stages:

1. **Ingest**: The reader connects to Telegram via MTProto and fetches messages from monitored channels.
2. **Process**: The pipeline filters, deduplicates, scores, and enriches messages with embeddings and metadata.
3. **Output**: The scheduler clusters ready items, selects top content, and renders the digest.
4. **Publish**: The bot posts the formatted digest to the target channel.

## Entry Points

The `cmd/digest-bot` binary serves as the single runtime entrypoint. The `--mode` flag selects which component to run:

- `--mode=bot`: Runs the admin bot for operator commands
- `--mode=reader`: Runs the MTProto reader to ingest messages
- `--mode=worker`: Runs the processing pipeline
- `--mode=digest`: Runs the digest scheduler (add `--once` for single execution)

Utility tools live under `cmd/tools/` and are separate from the main runtime.

## Dependency Rules

The architecture enforces clear dependency boundaries to prevent coupling:

- **app** depends on all domain packages and wires them together
- **bot** depends on `storage`, `platform`, `core/llm`, and defines a `DigestBuilder` interface
- **ingest/reader** depends on `storage`, `platform`, and `core/links`
- **process** depends on `storage`, `platform`, `core/llm`, and `core/links`
- **output/digest** depends on `storage`, `platform`, and `core/llm`
- **core** depends on `platform` and storage interfaces (not concrete implementations)
- **storage** has no dependencies on other domain packages
- **platform** has no dependencies on other domain packages

Import cycles are prohibited and enforced via linting.

## Consumer-Defined Interfaces

Following Go idiom, interfaces are defined by the packages that consume them rather than the packages that implement them. The `storage` package implements these interfaces without importing the consumer packages.

### Interface Locations

| Package | Interface | Purpose |
| --- | --- | --- |
| `internal/bot` | `Repository` | Storage operations for admin commands |
| `internal/bot` | `DigestBuilder` | Digest preview without importing output |
| `internal/ingest/reader` | `Repository` | Channel and message storage for ingestion |
| `internal/process/pipeline` | `Repository` | Message processing and item storage |
| `internal/output/digest` | `Repository` | Digest storage and item retrieval |
| `internal/core/links` | `LinkCacheRepository` | Link resolution caching |
| `internal/core/links` | `ChannelRepository` | Channel lookup for link resolution |

### Compile-Time Interface Assertions

Each consumer package includes a compile-time assertion to verify that `*storage.DB` (or an adapter) implements its interface:

```go
// In internal/bot/repository.go
var _ Repository = (*db.DB)(nil)

// In internal/process/pipeline/pipeline.go
var _ Repository = (*db.DB)(nil)

// In internal/output/digest/repository.go
var _ Repository = (*db.DB)(nil)

// In internal/ingest/reader/repository.go
var _ Repository = (*db.DB)(nil)

// In internal/storage/links_adapter.go (for adapted interfaces)
var _ links.ChannelRepository = (*ChannelRepoAdapter)(nil)
var _ links.LinkCacheRepository = (*DB)(nil)
```

These assertions catch interface mismatches at compile time rather than runtime.

### Breaking Circular Dependencies

The `DigestBuilder` interface in `internal/bot/digest_builder.go` breaks what would otherwise be a circular dependency between `bot` and `output/digest`:

```go
// DigestBuilder provides digest building capabilities for preview commands.
// This interface is implemented by output/digest.Scheduler.
type DigestBuilder interface {
    BuildDigest(ctx context.Context, start, end time.Time) (string, []db.Item, []db.ClusterWithItems, interface{}, error)
}
```

The `app` layer injects the concrete `*digest.Scheduler` as the `DigestBuilder` when constructing the bot.

## App Layer Wiring

The `internal/app` package serves as the composition root. It constructs all dependencies and injects concrete implementations:

```go
// Bot mode: inject digest scheduler as DigestBuilder
digestBuilder := digest.New(cfg, database, nil, llmClient, logger)
b, err := bot.New(cfg, database, digestBuilder, llmClient, logger)

// Digest mode: inject bot as DigestPoster
b, err := bot.New(cfg, database, nil, llmClient, logger)
s := digest.New(cfg, database, b, llmClient, logger)

// Worker mode: inject link resolver
resolver := links.New(cfg, database, channelAdapter, nil, logger)
p := pipeline.New(cfg, database, llmClient, resolver, logger)
```

This pattern keeps domain packages decoupled while allowing the app layer to compose them as needed.

## Storage Adapters

When a consumer interface does not match the `*storage.DB` methods directly, the storage package provides thin adapters:

- `ChannelRepoAdapter` adapts `*DB` to implement `links.ChannelRepository`

These adapters live in `internal/storage/` and keep interface adaptation logic close to the implementation.

## Adding New Features

When adding new functionality:

1. Identify which domain package owns the feature
2. Define any required repository methods in that package's interface
3. Implement the methods in `internal/storage`
4. Add a compile-time assertion if creating a new interface
5. Wire dependencies in `internal/app` if needed

This approach keeps changes localized and maintains clear package boundaries.
