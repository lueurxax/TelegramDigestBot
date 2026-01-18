# Digest Schedule & Timezones

The digest scheduler supports timezone-aware schedules so you can control **when** digests are sent without losing content. When a schedule is configured, each digest covers the exact interval since the previous scheduled send.

## Overview

- Schedules are defined per **weekday** and **weekend**.
- Times are **hour-only** (`H:00` or `HH:00`).
- Windows are derived from the schedule itself (no fixed `digest_window`).
- Timezone is stored with the schedule (IANA names).

## Commands

```
/schedule timezone Europe/Kyiv
/schedule weekdays times 09:00,13:00
/schedule weekdays hourly 18:00-21:00
/schedule weekends hourly 06:00-22:00
/schedule preview [count]
/schedule show
/schedule clear
```

## Schedule Format

Stored in `settings` under the key `digest_schedule`:

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

Rules:
- **Weekdays** = Monday-Friday, **Weekends** = Saturday-Sunday.
- `times` and `hourly` are merged and deduplicated.
- `hourly` expands to on-the-hour times within the inclusive range.
- If a day group is empty, no digests are scheduled for that group.

## Window Behavior

When a schedule exists:
- Each window is `[previous_scheduled_time, scheduled_time)` in local time.
- The scheduler stores a `digest_schedule_anchor` timestamp to avoid reprocessing.
- The scheduler tick interval only controls how often checks run.

If no schedule exists:
- The scheduler uses the legacy `digest_window`.

## Validation

- Times must be `H:00` or `HH:00` (24h).
- Invalid times (e.g., `09:30`, `25:00`) are rejected.
- Invalid timezones are rejected with an explicit error.
- Hourly ranges cannot cross midnight (split into two ranges if needed).

## Tips

- Use `/schedule preview` to verify the next send times.
- Use `/schedule clear` to revert to `digest_window`.
