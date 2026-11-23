# ログ 1 行の詳細 (JSON のキーと値、書いた goroutine と結びつきの確かさ)

- `service`・`log` (必須)。`log` は `search-logs` の ID

書いた goroutine が分かれば、その `instance`・`goroutine` と `log` を `diagnose-goroutine` に渡す。分からなければ、その時刻の `summarize-goroutines` を見る。
