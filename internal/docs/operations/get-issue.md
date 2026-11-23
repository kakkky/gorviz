# Issue の詳細 (発生の回数の推移、発生ごとの影響の大きかった goroutine、ネクストアクション)

- `service`・`issue` (必須)。`issue` は `list-issues` の ID
- `from`・`to`
  - 期間

発生ごとの `occurrence` (時刻) と `instance` を `diagnose-goroutine` の `issue`・`occurrence` に渡すと、発生の前後を調べる。
