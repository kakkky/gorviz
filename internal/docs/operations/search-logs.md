# Search the logs (by period, level, instance, JSON key and value, and text)

- `service` (required)
- `from`, `to`
  - the period. The last hour when left out
- `level`
  - this level and above (`DEBUG`, `INFO`, `WARN`, `ERROR`)
- `instance`
  - only this instance
- `attr`
  - a JSON key and value as `key=value`. Can be passed several times
- `q`
  - text contained in the message or the raw line
- `cursor`, `max-bytes`
  - how to get the rest

Pass the `log` of a row to `get-log`. Get a feel for the counts with `count-logs` first.
