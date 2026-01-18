# Repository Guidelines

## Project Structure & Module Organization
- `cmd/digest-bot/` holds the main entrypoint; run modes are selected via `--mode`.
- `cmd/tools/` contains utility binaries (eval, labels).
- `internal/` contains domain-organized packages:
  - `internal/app/` - runtime wiring and service startup
  - `internal/bot/` - admin bot and operator commands
  - `internal/core/` - shared services (llm, links, domain types)
  - `internal/ingest/reader/` - Telegram MTProto reader
  - `internal/output/digest/` - clustering, selection, rendering
  - `internal/process/` - pipeline, filters, dedup
  - `internal/platform/` - config, observability, htmlutils
  - `internal/storage/` - DB, repositories, sqlc, migrations
- `internal/**/**/*_test.go` contains unit tests colocated with the packages they cover.
- `deploy/compose/` and `deploy/k8s/` contain Docker Compose and Kubernetes manifests.
- `migrations/` and `sqlc.yaml` define database schema changes and SQL codegen.
- `docs/` holds architecture and design references.
- `data/` stores runtime artifacts like Telegram session files.

## Build, Test, and Development Commands
- `go test ./...` runs all Go tests across the repository.
- `go run ./cmd/digest-bot/main.go --mode=bot` starts the control bot locally.
- `go run ./cmd/digest-bot/main.go --mode=reader` starts the MTProto reader.
- `go run ./cmd/digest-bot/main.go --mode=worker` runs the processing pipeline.
- `go run ./cmd/digest-bot/main.go --mode=digest` runs the digest scheduler.
- `docker-compose -f deploy/compose/docker-compose.yml up -d` starts the full stack.

## Coding Style & Naming Conventions
- Follow standard Go conventions and run `gofmt` on touched files.
- Use lower-case package names (e.g., `htmlutils`) and `MixedCaps` for exported identifiers.
- Keep configuration in `.env` (see `.env.example`) and avoid hardcoding secrets.

## Testing Guidelines
- Tests live next to the code under `internal/` and use the Go testing package.
- Name tests `TestXxx` in `*_test.go` files; table-driven tests are preferred for variants.
- No explicit coverage target is defined; add tests for new logic or bug fixes.

## Commit & Pull Request Guidelines
- History suggests short, imperative, sentence-case messages (e.g., "Fix deduplication...").
- Keep commits focused on one logical change; avoid mixing refactors with behavior changes.
- Run `make lint` before every commit to keep linting consistent.
- PRs should include a concise summary, key config changes, and how to verify (`go test ./...` or specific modes).

## Security & Configuration Tips
- Store credentials in `.env` and keep local session files under `data/`.
- When changing schema or SQL, update `migrations/` and regenerate via `sqlc` if needed.

## Channel Discovery System

The discovery system tracks channels found through forwards, links, and mentions in tracked channels.

### Key Files
- `internal/storage/discovery.go` - Discovery CRUD and filtering logic
- `internal/storage/channels.go` - Channel management, `markDiscoveryAdded` links discoveries to channels
- `internal/bot/handlers.go` - Bot commands: `/discover`, `/discover stats`, `/discover cleanup`, `/discover show-rejected`
- `internal/app/app.go` - Background reconciliation job (startup + every 6 hours)
- `internal/storage/queries.sql` - All discovery-related SQL queries

### Identity Model
Channels have multiple identifiers: `username`, `tg_peer_id`, `invite_link`. The `matched_channel_id` column links discovery rows to tracked channels. Rejection cascades to all rows sharing any identifier.

### Admin Commands
- `/discover` - List pending discoveries with approve/reject buttons
- `/discover approve @user` - Add channel to tracking
- `/discover reject @user` - Reject (hides from list, cascades to related rows)
- `/discover show-rejected [limit]` - View rejected discoveries
- `/discover cleanup` - Backfill `matched_channel_id` for existing rows
- `/discover stats` - Show counts and filter statistics

## SQL & sqlc Workflow

### Modifying Queries
1. Edit `internal/storage/queries.sql`
2. Run `~/go/bin/sqlc generate` (or `sqlc generate` if in PATH)
3. Update Go code in `internal/storage/` to match new signatures
4. Run `go build ./...` to verify

### PostgreSQL Patterns Used
- **CTE for cascading updates** - `WITH target AS (...) UPDATE ... FROM target`
- **DISTINCT ON** - Deduplicate rows keeping highest-scoring one per group
- **NOT EXISTS subquery** - Filter out rows matching tracked channels

## Linting Guidelines

Run `~/go/bin/golangci-lint run ./...` before committing.

### Common Issues and Fixes
| Issue | Fix |
|-------|-----|
| gocyclo (complexity > 10) | Extract logic into helper functions |
| nestif (nested blocks) | Use early returns, extract to functions |
| mage (magic numbers) | Define named constants |
| err113 (dynamic errors) | Use `var errFoo = errors.New(...)` and wrap with `%w` |
| goconst (repeated strings) | Extract to constants |
| wsl_v5 (whitespace) | Add blank line after variable declarations before if/for |

### Never Do
- Add `nolint` comments
- Add exclusions to `.golangci.yml`
- Ignore lint errors
