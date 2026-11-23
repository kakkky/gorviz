# One log line in detail (its JSON keys and values, and the goroutine that wrote it with the certainty)

- `service`, `log` (required). `log` is an ID from `search-logs`

When the goroutine that wrote it is known, pass its `instance`, `goroutine` and the `log` to `diagnose-goroutine`. When it is not known, look at `summarize-goroutines` at that time.
