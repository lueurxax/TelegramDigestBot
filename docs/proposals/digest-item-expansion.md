# On-Demand Item Expansion and Q&A

> **Status: PROPOSAL** (January 2026)
>
> Goal: add a short link per digest item that opens an expanded view with evidence, context, and optional Q&A.

## Summary
Add an on-demand expansion link near each digest item. The expanded view shows full context, evidence sources, and allows the user to ask questions. To avoid LLM costs, the default path uses existing evidence and provides a deep link to external ChatGPT (user subscription) for Q&A. Optional server-side Q&A can be enabled with strict budgets.

## Goals
- Provide deeper context without increasing baseline LLM usage.
- Enable user-driven Q&A for specific items.
- Keep access restricted to bot admins.

## Non-Goals
- Public access or multi-user research portal.
- Real-time chat hosting in the bot itself.

## UX
### Digest Output
- Add a short link at the end of each item:
  - `More: https://digest.local/i/<short_id>`

### Expanded Item View
- Raw message text + summary + scores.
- Evidence list (sources + agreement scores).
- Corroborating channels and cluster context.
- Button: “Open in ChatGPT” (pre-filled prompt).
- Optional: “Ask here” if server-side Q&A is enabled.

## Implementation Details

### 1) Short Link Generation
- Deterministic short ID derived from item ID.
- Link resolves to a read-only item view.

### 2) Context Assembly (No New LLM)
- Use existing evidence + link previews + cluster summary.
- Show claim matches and agreement scores.

### 3) ChatGPT Deep Link
- Build a prompt with item summary + evidence list.
- Open external ChatGPT with the prompt (user’s subscription).

### 4) Optional Server-Side Q&A
- Guarded by budgets and RPM limits.
- Uses existing LLM task chain and `LLM_MODEL_RPM`.

## Configuration
```bash
EXPANDED_VIEW_ENABLED=true
EXPANDED_VIEW_BASE_URL=https://digest.local
EXPANDED_VIEW_REQUIRE_ADMIN=true
EXPANDED_QA_ENABLED=false
```

## Success Criteria
- Users can access expanded context in 1 click.
- No increase in baseline LLM cost.
- Q&A only incurs cost when explicitly requested.

## Open Questions
- Should expanded views be indexed or kept private-only?
- Should prompts include raw message text or only summary+evidence?
