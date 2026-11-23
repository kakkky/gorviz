# How long each kind of data is kept, and what comes back after that

Default periods (the server's config changes them):

- Execution traces
  - 7 days. After that, the scheduler replay and the exact diagnosis over the period of a request are no longer possible
- Execution traces kept around Issue occurrences and error logs
  - 31 days
- Metrics, Issues, logs, the goroutines that wrote logs, instance starts and ends, goroutine snapshots, leak checks, and per-minute time-by-state breakdowns
  - 31 days

Fields past their retention are not left out; they come back as not available. When no data is left, the nearest kept data (such as traces kept around an Issue or an error log) comes back instead. `list-available-data` tells which data is kept for a period.
