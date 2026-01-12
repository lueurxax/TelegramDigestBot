# Repository Guidelines

## Project Structure & Module Organization
- `cmd/digest-bot/` holds the main entrypoint; run modes are selected via `--mode`.
- `internal/` contains the core Go packages (pipeline, filters, htmlutils, telegram bot, etc.).
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
