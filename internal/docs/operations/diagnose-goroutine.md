# Diagnose a goroutine (time by state, main cause, where it stopped, leak; with the certainty and what it was computed from)

- `service`, `instance`, `goroutine` (required)
- The period to look at is decided by the entry point. Pass one of these
  - `log`
    - a log ID. The request it ended when it has a duration, otherwise around the time it was written
  - `issue` and `occurrence`
    - around an Issue occurrence
  - `woken`
    - the ID of the goroutine that was woken. The period that goroutine waited (to look at the goroutine that woke it)
  - `from`, `to`
    - the period as is

Follow the IDs that come back, such as `wokenBy`. Read `main-causes` for the main causes and `certainty` for the certainty.
