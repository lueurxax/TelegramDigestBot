# Evaluation Harness

This directory holds labeled datasets for offline quality evaluation.

## Dataset Format (JSONL)

Each line is a JSON object with the following fields:

```json
{"id":"item-1","label":"good","relevance_score":0.86,"importance_score":0.42}
```

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Optional identifier |
| `label` | string | `good`, `bad`, or `irrelevant` (also accepts `rating`) |
| `relevance_score` | float | Model-assigned relevance score |
| `importance_score` | float | Model-assigned importance score |

## Annotation Workflow

```
/annotate enqueue [hours] [limit]   # Queue samples
/annotate next                       # Get next item to label
/annotate label <good|bad|irrelevant> [comment]
/annotate skip                       # Skip without labeling
/annotate stats                      # Check progress
```

See [Annotations](../features/annotations.md) for the complete annotation system documentation.

## Export Labeled Set

```bash
go run ./cmd/tools/labels -out docs/eval/golden.jsonl -limit 200
```

Requires `POSTGRES_DSN` (or pass `-dsn`).

## Run the Harness

```bash
go run ./cmd/tools/eval -input docs/eval/sample.jsonl -relevance-threshold 0.5 -importance-threshold 0.3
```

| Flag | Description |
|------|-------------|
| `-ignore-importance` | Evaluate relevance only |
| `-min-precision` | Fail if precision below threshold |
| `-max-noise-rate` | Fail if noise rate above threshold |

## Notes

- The harness does not call the LLM. It evaluates the scores already present in the dataset.
- For CI, point `-input` at a curated dataset (e.g., `docs/eval/golden.jsonl`).
