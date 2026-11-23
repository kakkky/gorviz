# ログの集計 (絞ったログを、キーごとに数えた件数)

- `service`・`by` (必須)。`by` は数えるキー (`level`・`instance`・`message` か、JSON のキー)
- `from`・`to`・`level`・`instance`・`attr`
  - `search-logs` と同じ絞り方
