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
| [Semantic Clustering](features/clustering.md) | Deduplication, coherence validation, and topic generation |
| [Digest Schedule](features/digest-schedule.md) | Timezone-aware scheduling and window configuration |
| [Channel Discovery](features/discovery.md) | Automatic channel discovery, keyword filters, admin review |
| [Channel Importance](features/channel-importance-weight.md) | Per-channel importance weighting |

### AI/LLM Configuration

| Document | Description |
|----------|-------------|
| [LLM Configuration](features/llm-configuration.md) | Multi-provider LLM system, model selection, cost tracking, budget controls |

### Digest Output

| Document | Description |
|----------|-------------|
| [Editor Mode](features/editor-mode.md) | Narrative rendering, tiered importance, consolidated clusters |
| [Bulletized Output](features/bulletized-output.md) | Claim extraction, deduplication, and bullet-based rendering |
| [Vision & Images](features/vision-images.md) | Vision routing, cover images, AI-generated covers |
| [Item Expansion](features/item-expansion.md) | Expanded item views with evidence, context, and ChatGPT Q&A |

### Enrichment & Verification

| Document | Description |
|----------|-------------|
| [Source Enrichment](features/source-enrichment.md) | Multi-provider evidence retrieval and agreement scoring |
| [Corroboration](features/corroboration.md) | Channel corroboration and fact-check links |
| [Link Enrichment](features/link-enrichment.md) | URL resolution and content extraction |
| [Link Seeding](features/link-seeding.md) | Seed external URLs from Telegram to crawler queue |

### Quality & Evaluation

| Document | Description |
|----------|-------------|
| [Pipeline Optimization](features/pipeline-optimization.md) | Heuristic filters, caching, summary post-processing |
| [Annotations](features/annotations.md) | Item labeling for quality evaluation and threshold tuning |
| [Evaluation Harness](eval/README.md) | Offline quality evaluation with labeled datasets |

### Research & Analytics

| Document | Description |
|----------|-------------|
| [Research Dashboard](features/research-dashboard.md) | Web UI and API for archive exploration and analytics |

## Proposals

These documents describe features under consideration or in development.

| Document | Status | Description |
|----------|--------|-------------|
| [LLM Budget Guardrails](proposals/llm-budget-guardrails.md) | Proposed | Per-model budgets and rate limits |
| [Research & Visualization](proposals/research-interactivity-visualization.md) | Implemented | Original proposal for research dashboard |
| [Telegram Link Seeding](proposals/telegram-link-seeding.md) | Implemented | Original proposal for link seeding |
| [Bulletized Output](proposals/digest-bulletized-output.md) | Implemented | Original proposal for bullet-based digests |

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
