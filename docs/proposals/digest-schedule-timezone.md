# Proposal: Timezone-Aware Digest Schedule

## Summary
Add a timezone-aware digest schedule so admins can control when digests are sent (e.g., no news after 22:00, resume at 06:00; weekends hourly; weekdays only twice during work and hourly after 18:00). Schedule times are hour-only (minutes must be `00`). The digest window size is derived from the schedule itself (time between scheduled send times). This avoids dropping any windows: each digest covers the entire period since the previous scheduled send.

## Goals
- Let admins define different schedules for weekdays vs weekends.
- Support quiet hours and custom times (HH:00 only).
- Respect a configurable IANA timezone stored in settings.
- Preserve all content by aggregating over the exact schedule cadence (no silent gaps).

## Non-Goals
- No per-channel schedules.
- No UI beyond bot commands.
- No changes to scoring or item selection logic.
- No fixed `digest_window` once the schedule is configured.

## User Experience
Bot commands (examples):
- `/schedule timezone Europe/Kyiv`
- `/schedule weekdays times 09:00,13:00,18:00,19:00,20:00,21:00`
- `/schedule weekdays hourly 18:00-21:00`
- `/schedule weekends hourly 06:00-22:00`
- `/schedule preview` (shows next N scheduled send times)
- `/schedule clear` (removes schedule and reverts to default behavior)
- `/schedule show`

Example policy matching the request:
- Timezone: `Europe/Kyiv`
- Weekdays: `09:00,13:00` and hourly from `18:00` to `21:00`
- Weekends: hourly from `06:00` to `22:00`

## Settings / Data Model
Store a single JSON schedule object in settings:

Key: `digest_schedule`

```json
{
  "timezone": "Europe/Kyiv",
  "weekdays": {
    "times": ["09:00", "13:00"],
    "hourly": {"start": "18:00", "end": "21:00"}
  },
  "weekends": {
    "hourly": {"start": "06:00", "end": "22:00"}
  }
}
```

Notes:
- Timezone defaults to `UTC` if missing/invalid.
- If `digest_schedule` is missing, scheduler behaves as today (every window).
- Either `times` or `hourly` can be omitted; missing sections result in no schedule for that day group.
- `times` entries follow the same `HH:00` format and are merged with hourly times.
- Weekdays = Monday-Friday; Weekends = Saturday-Sunday.

## Scheduler Logic
- Parse `digest_schedule` and timezone at startup or per run.
- Build the ordered list of scheduled send times for the local timezone within the catch-up range.
- Catch-up range is `digest_catchup_hours` (default: 24 hours).
- Each digest window is `[previous_scheduled_time, scheduled_time)` in local time (converted to UTC for queries).
- This replaces `digest_window` when a schedule is present; window size is the gap between scheduled times.
- Quiet hours are implicit: if no scheduled times fall inside a range, no digest is produced.
- Log the computed windows and any gaps for observability (debug).
- The scheduler tick interval (currently `digest_interval`/config tick) remains unchanged; it only controls how often the scheduler checks, not the window size.
- Partial updates merge by mode: setting `weekdays times` replaces only `times` for weekdays; setting `weekdays hourly` replaces only `hourly` and preserves existing times.

### Hourly Semantics
`hourly.start`/`hourly.end` defines on-the-hour times within the inclusive range, e.g. `18:00-21:00` -> `18:00, 19:00, 20:00, 21:00`.

### Merge & Dedup Rules
- Merge `times` and `hourly` into a single list, then deduplicate.
- `times` entries follow the `HH:00` format and are merged with hourly times.

## Edge Cases
- DST transitions: use `time.LoadLocation` with IANA names and compare local times.
- Irregular gaps: window size can vary (e.g., 13:00 -> 18:00). This is expected and should not drop content.
- Catch-up: compute scheduled windows inside the catch-up range and process each sequentially.
- First window boundary: use the last scheduled time before `now` within the catch-up range; if none, start at `now` and log a warning that earlier windows are skipped.
- Schedule changes: apply immediately on the next scheduler tick; windows are generated from the latest schedule.

## Validation
- Time format: must be `HH:00` (24h); reject `9:00`, `25:00`, `09:30`, `09:00:00`.
- Timezone: must be valid IANA name; reject invalid values.
- Empty schedule: if both `times` and `hourly` are absent/empty for a day group, that group has no digests.
- Hourly range crossing midnight (e.g., `22:00-06:00`) is invalid; require admins to split into two ranges across day groups.
- Error messages should be explicit, e.g. `Invalid time format "09:30" - use HH:00`.

## Migration Plan
1) Add schedule parsing and window generation helpers.
2) Add settings key `digest_schedule` and bot commands to update it.
3) Wire into digest scheduler to generate windows from the schedule and ignore `digest_window` when schedule is set.
4) Add tests for weekday/weekend and timezone behavior.

## Testing
- Unit tests for schedule parsing and matching logic.
- Test DST boundary behavior (fixed dates).
- Scheduler integration test to ensure windows are skipped/published correctly.

## Future Extensions
- Per-day overrides (e.g., different Friday schedule).
- Cron-like syntax for advanced schedules (non-goal for v1).

## Success Criteria
- Digest output only during the configured local-time schedule.
- Admins can update schedule via bot without deploys.
- No regressions in digest content or scoring.
