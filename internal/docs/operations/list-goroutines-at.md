# The goroutine snapshot nearest to a time, grouped by creator and the place they stop

- `service`, `instance`, `at` (required). `at` is RFC 3339
- `cursor`, `max-bytes`

Pass the goroutine IDs of a group to `diagnose-goroutine`.
