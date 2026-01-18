# Project Rules

- **NEVER touch .golangci.yml** - no edit, no write, no git checkout, no restore, NOTHING. If lint shows unexpected issues, report to user and do NOT attempt to fix or restore the file.

## Linting Philosophy

Linters keep code clean, readable, and maintainable. They catch:
- Unhandled errors
- Functions too long to read
- Magic numbers that should be constants
- Complex nested blocks
- And many other code quality issues

**CRITICAL RULES:**
1. **NEVER add exclusions or nolint comments** - this HIDES problems instead of FIXING them
2. **FIX lint issues properly** - refactor long functions, handle errors, extract constants
3. User is removing exclusions step-by-step - when new issues appear, fix them correctly
4. If there are too many issues to fix in one session, fix as many as possible and report progress

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

### Key Components
- `internal/storage/discovery.go` - Discovery recording, filtering, and status management
- `internal/storage/channels.go` - Channel management and `markDiscoveryAdded` for identity linking
- `internal/bot/handlers.go` - Admin commands (`/discover`, `/discover stats`, `/discover cleanup`)
- `internal/app/app.go` - Reconciliation job (runs at startup + every 6 hours)

### Identity Matching
Channels can be identified by multiple identifiers (username, `tg_peer_id`, invite_link). The system:
- Uses `matched_channel_id` to link discovery rows to tracked channels
- Cascades rejection across all rows sharing any identifier (CTE in `UpdateDiscoveryStatusByUsername`)
- Uses `DISTINCT ON` in `GetPendingDiscoveries` to deduplicate rows for the same channel

### Source Hygiene
Discoveries are skipped when:
- Source channel is inactive
- Discovery is self-referential (same `tg_peer_id`, username, or invite_link as source)

### Configurable Thresholds
- `discovery_min_seen` (default: 2) - Minimum discovery count
- `discovery_min_engagement` (default: 50) - Minimum engagement score
- Settings stored in `settings` table, editable via `/settings`

## SQL & sqlc Patterns

### Regenerating sqlc
After modifying `internal/storage/queries.sql`:
```bash
~/go/bin/sqlc generate
```

### CTE for Cascading Updates
Use CTEs when an update needs to affect related rows:
```sql
WITH target AS (
    SELECT src.username AS t_username, src.tg_peer_id AS t_peer_id
    FROM discovered_channels src
    WHERE src.username = $1
    LIMIT 1
)
UPDATE discovered_channels dc
SET status = $2
FROM target
WHERE dc.username = target.t_username OR dc.tg_peer_id = target.t_peer_id;
```

### DISTINCT ON for Deduplication
PostgreSQL-specific pattern for keeping one row per group:
```sql
SELECT * FROM (
  SELECT DISTINCT ON (dc.username)
    dc.id, dc.username, ...
  FROM discovered_channels dc
  ORDER BY dc.username, dc.engagement_score DESC
) deduped
ORDER BY engagement_score DESC;
```

## Common Lint Fixes

### Cyclomatic Complexity (gocyclo)
When complexity exceeds 10, extract logical blocks into helper functions:
- `shouldSkipDiscovery` → extracted `shouldSkipForSourceHygiene` and `isSelfDiscovery`
- `handleDiscoverNamespace` → extracted `handleDiscoverApproveCmd`, `handleDiscoverRejectCmd`, etc.

### Nested Blocks (nestif)
Flatten nested conditionals by:
- Early returns for error/skip cases
- Extracting nested logic into separate functions

### Magic Numbers (mage)
Extract repeated numbers into named constants in the `const` block.

### Dynamic Errors (err113)
Define static error variables and wrap them:
```go
var errInvalidItemID = errors.New("invalid item id")
// Usage:
return fmt.Errorf("%w: %s", errInvalidItemID, itemID)
```
