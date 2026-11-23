# One Issue in detail (occurrences over time, the goroutines each occurrence hit the most, where to look next)

- `service`, `issue` (required). `issue` is an ID from `list-issues`
- `from`, `to`
  - the period

Pass the `occurrence` (time) and `instance` of an occurrence to `diagnose-goroutine` as `issue` and `occurrence` to look around it.
