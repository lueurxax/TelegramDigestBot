# Documentation Index

This directory contains all documentation for the Telegram Digest Bot project.

## Getting Started

- [README](../README.md) - Project overview, setup, and quick start
- [Architecture](architecture.md) - Package structure and dependency patterns
- [Technical Design](technical-design.md) - System components and data flow

## Feature Documentation

### Core Features

| Document | Description |
|----------|-------------|
| [Content Quality](features/content-quality.md) | Relevance gates, feedback loops, clustering, topic balance |
| [Digest Schedule](features/digest-schedule.md) | Timezone-aware scheduling and window configuration |
| [Channel Discovery](features/discovery.md) | Automatic channel discovery, keyword filters, admin review |
| [Channel Importance](features/channel-importance-weight.md) | Per-channel importance weighting |

### Digest Output

| Document | Description |
|----------|-------------|
| [Editor Mode](features/editor-mode.md) | Narrative rendering, tiered importance, consolidated clusters |
| [Vision & Images](features/vision-images.md) | Vision routing, cover images, AI-generated covers |

### Enrichment & Verification

| Document | Description |
|----------|-------------|
| [Source Enrichment](features/source-enrichment.md) | Multi-provider evidence retrieval and agreement scoring |
| [Corroboration](features/corroboration.md) | Channel corroboration and fact-check links |
| [Link Enrichment](features/link-enrichment.md) | URL resolution and content extraction |

### Quality & Evaluation

| Document | Description |
|----------|-------------|
| [Annotations](features/annotations.md) | Item labeling for quality evaluation and threshold tuning |
| [Evaluation Harness](eval/README.md) | Offline quality evaluation with labeled datasets |

## Proposals

These documents describe features under consideration or in development.

| Document | Status | Description |
|----------|--------|-------------|
| [Source Enrichment](proposals/source-enrichment-fact-checking.md) | Implemented | Original design for Phase 1 and Phase 2 enrichment |
| [Language Routing](proposals/enrichment-language-routing.md) | Implemented | Language-aware search query routing |
| [Search Infrastructure](proposals/search-infrastructure-redesign.md) | Ready | YaCy replacement with SolrCloud (single-step deployment) |
| [Research & Visualization](proposals/research-interactivity-visualization.md) | Proposal | Interactive research UI and analytics |

## Other

- [Privacy Policy](privacy-policy.md) - Data collection, usage, and retention policies

## Development

For development guidelines, see [CLAUDE.md](../CLAUDE.md) which includes:

- Project structure and module organization
- Build, test, and development commands
- Coding style and naming conventions
- Commit and pull request guidelines
- SQL and sqlc patterns
- Linting guidelines
