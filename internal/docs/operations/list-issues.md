# The Issues (kind, subject, certainty, number of occurrences, last occurrence, ongoing or not)

- `service` (required)
- `from`, `to`
  - the period (RFC 3339). The last 31 days when left out
- `cursor`, `max-bytes`
  - how to get the rest. Results are cut by size

Pass the `issue` of a row to `get-issue`.
