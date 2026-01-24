# Agents

This project uses specialized Claude agents for specific tasks. Agent definitions are located in `.claude/agents/`.

## Available Agents

| Agent | Purpose |
|-------|---------|
| `k8s-log-analyzer` | Kubernetes log analysis for production debugging |

## Using Agents

Agents are invoked automatically when their trigger conditions are met, or manually via the Task tool.

## Repository Guidelines

See [CLAUDE.md](CLAUDE.md) for complete project guidelines including:

- Project structure and module organization
- Build, test, and development commands
- Coding style and naming conventions
- Commit and pull request guidelines
- Channel discovery system documentation
- SQL and sqlc patterns
- Linting guidelines and common fixes
