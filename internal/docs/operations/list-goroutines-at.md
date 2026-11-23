# ある時刻に近い goroutine のスナップショットを、生成元と止まっている場所ごとにまとめたもの

- `service`・`instance`・`at` (必須)。`at` は RFC 3339
- `cursor`・`max-bytes`

各まとまりの goroutine の ID を `diagnose-goroutine` に渡す。
