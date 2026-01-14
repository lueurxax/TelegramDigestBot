# Proposal: Architecture and Structure Refactor

## Problem Statement

The repository has grown organically: multiple entrypoints in `cmd/`, overlapping package boundaries inside `internal/`, and configuration/setting logic spread across modules. This makes it harder to reason about ownership, introduce new features safely, and onboard contributors.

## Goals

- Make runtime flows (ingest -> process -> digest -> publish) explicit.
- Reduce cross-package coupling and implicit dependencies.
- Provide a clear, stable directory layout for future changes.
- Improve readability without changing external behavior.

## Non-Goals

- No functional changes to digest logic or bot behavior.
- No schema changes beyond refactor support.
- No large-scale rewrite of LLM prompts or scoring.

## Proposed Structure

Keep the existing Go module but reorganize packages under `internal/` into clear domains:

- `internal/app/`: runtime wiring and service startup (bot/reader/worker/digest).
- `internal/ingest/`: Telegram reader and raw message intake.
- `internal/process/`: pipeline, filters, dedup, relevance gate, embeddings.
- `internal/output/`: clustering, digest selection, digest rendering.
- `internal/bot/`: admin bot and operator-facing commands.
- `internal/storage/`: DB, repositories, sqlc, migrations helpers.
- `internal/platform/`: config, observability, shared utils (e.g., htmlutils).
- `internal/core/`: shared domain services (LLM client, link resolver, link extraction) used by ingest/process/output.

### Current → Proposed Mapping

| Current Package | Proposed Package |
| --- | --- |
| `internal/telegramreader` | `internal/ingest` |
| `internal/linkextract` | `internal/core/links/linkextract` |
| `internal/linkresolver` | `internal/core/links` |
| `internal/pipeline`, `internal/filters`, `internal/dedup` | `internal/process` |
| `internal/digest` (selection + rendering) | `internal/output` |
| `internal/telegrambot` | `internal/bot` |
| `internal/db`, `internal/db/sqlc` | `internal/storage` |
| `internal/config`, `internal/observability`, `internal/htmlutils` | `internal/platform` |
| `internal/llm` | `internal/core/llm` (see below) |

### LLM Package Placement

Place `internal/llm` under `internal/core/llm` and keep it storage-agnostic by depending on narrow consumer-defined interfaces instead of `db.DB`. For example:
- `type SettingsStore interface { GetSetting(ctx, key string, target interface{}) error }`
- `type ChannelStatsProvider interface { GetChannelStats(ctx context.Context) (map[string]core.ChannelStats, error) }`

Define shared domain types (e.g., `core.ChannelStats`) inside `internal/core` to avoid importing storage types.
This keeps `core` independent of concrete storage while serving both `process` and `output` packages.

### Constants Consolidation

Defer consolidation until after the package moves stabilize. Keep constants close to their users during the refactor to reduce churn, and only consolidate if duplication becomes painful after the new structure is in place.

## Entry Points

Consolidate the runtime binaries:

- Keep `cmd/digest-bot` as the single runtime entrypoint with `--mode`.
- Move utility tools into `cmd/tools/*` (`eval`, `labels`, etc.).
- Deprecate redundant entrypoints in `cmd/bot`, `cmd/reader`, `cmd/worker`, `cmd/digest`.

## Dependency Rules

- Interfaces are defined by consumers; `storage` implements them.
- `internal/core` depends on storage interfaces (not concrete db) and `platform`.
- `internal/ingest` depends on `storage`, `platform`, and `core/links`.
- `internal/process` depends on `storage`, `platform`, and `core/llm`/`core/links`.
- `internal/output` depends on `storage`, `platform`, and `core/llm`.
- `internal/bot` depends on `storage`, `platform`, and `core` (for model settings), not on `process` internals.
- `internal/bot` uses a `DigestBuilder` interface (defined in bot) for digest preview; the app layer injects `output/digest.Scheduler`.
- No circular imports; enforce via `golangci-lint` rules.

## Interface Extraction (Consumer-Defined)

Define interfaces at the consumer boundaries (Go idiom) rather than in `storage`. Example ownership:

- `internal/process`: `ItemRepository`, `MessageRepository`, `SettingsStore`
- `internal/output`: `DigestRepository`, `SettingsStore`
- `internal/ingest`: `ChannelRepository`, `MessageRepository`, `SettingsStore`
- `internal/bot`: `ChannelRepository`, `SettingsStore`, `RatingStore`

Each interface should be minimal and match current call sites; `db.DB` implements them via compile-time checks in the consumer packages.

Suggested locations:
- `internal/process/repository.go` (ItemRepository, MessageRepository, SettingsStore)
- `internal/output/repository.go` (DigestRepository, SettingsStore)
- `internal/ingest/repository.go` (ChannelRepository, MessageRepository, SettingsStore)
- `internal/bot/repository.go` (ChannelRepository, SettingsStore, RatingStore)

## Migration Plan

1) **Inventory**: document current package responsibilities and call graph; capture baseline behavior.
2) **Scaffold**: create new directories with shim packages; move code with minimal logic changes.
3) **Consumer interfaces**: define interfaces in the consumer packages and adapt `db.DB` to implement them.
4) **Rename and rewire**: update imports and entrypoints; keep config/env keys stable.
5) **Stabilize**: update tests and fix lint; verify with `make lint` and `go test ./...`.
6) **Cleanup**: remove deprecated packages and redundant entrypoints.

## Risks and Mitigations

- Risk: refactor breaks runtime wiring. Mitigation: phase changes and keep a working main branch.
- Risk: hidden dependency cycles. Mitigation: introduce interfaces and add linting rules early.
- Risk: deployment drift. Mitigation: keep `cmd/digest-bot` flags unchanged.

## Test Strategy

- Capture a baseline run of `go test ./...` before structural moves.
- Add targeted tests for moved packages if coverage gaps appear.
- Keep a small golden-path integration test (e.g., pipeline -> digest) to catch wiring regressions.

## Success Criteria

- Contributors can locate code by domain quickly.
- New features require fewer cross-package changes.
- CI passes without special-case lint excludes for moved code.
