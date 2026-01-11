# Evaluation Harness

This directory holds labeled datasets for offline quality evaluation.

## Dataset Format (JSONL)

Each line is a JSON object with the following fields:

```
{"id":"item-1","label":"good","relevance_score":0.86,"importance_score":0.42}
```

Fields:
- `id` (string): optional identifier.
- `label` (string): `good`, `bad`, or `irrelevant`. (`rating` is also accepted.)
- `relevance_score` (float): model-assigned relevance score.
- `importance_score` (float): model-assigned importance score.

## Run the Harness

```
go run ./cmd/eval -input docs/eval/sample.jsonl -relevance-threshold 0.5 -importance-threshold 0.3
```

Optional flags:
- `-ignore-importance` to evaluate relevance only.
- `-min-precision` / `-max-noise-rate` to fail when thresholds are violated.

## Notes

- The harness does not call the LLM. It evaluates the scores already present in the dataset.
- For CI, point `-input` at a curated dataset (e.g., `docs/eval/golden.jsonl`).
