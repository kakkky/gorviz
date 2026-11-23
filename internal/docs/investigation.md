# How to investigate from each entry point (leaks, locks, error logs, slow requests, time ranges)

- Goroutines pile up
  - `list-issues` for `GOROUTINE_LEAK`, then `get-issue` for "N of the M goroutines waiting at the same place leaked" per stack and their creator, then `diagnose-goroutine` for the wait reason and the certainty of the leak
- Waiting for locks
  - `list-issues` for `LOCK_CONTENTION`, then `get-issue` for the count, average and max per function that called `Lock` and the function that held it, then follow the goroutine that held it with `diagnose-goroutine` (`woken`)
- An error log
  - `search-logs` (`level=ERROR`), then `get-log` for the goroutine that wrote it, then `diagnose-goroutine` (`log`). The trace around error logs is kept long
- A slow request
  - `get-log` on the log line with its duration, then `diagnose-goroutine` (`log`). The period becomes that request. If the main cause is `RUNNABLE`, look for `CPU_CONTENTION` in the same time; if it is `GC`, look for `GC_PRESSURE`
- A time range found in your APM
  - `get-overview` (`from`, `to`). If there is an Issue, follow it as above. If not, see the main cause per creator with `summarize-goroutines`. If the runtime is not the cause, look at the app layer in your APM

Lists return only the key points and the IDs to look at next; get the details with the detail operations. Results are cut by size, and `cursor` gets the rest.
