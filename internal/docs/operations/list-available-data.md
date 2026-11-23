# Which data (trace chunks, per-minute time-by-state breakdowns, snapshots) is kept for a period

- `service`, `instance` (required)
- `from`, `to`
  - the period

Use it to find the nearest kept data when something is past its retention. Read `data-retention` for how long each kind is kept.
