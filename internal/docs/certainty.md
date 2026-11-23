# 確かさ (FACT / INFERENCE) と「何から算出したか」の読み方

- `FACT`
  - runtime の判定や trace の記録から直接言える
- `INFERENCE`
  - gorviz が推移や比較から推し量った。事実として扱わず、GUI の再生か別の操作で確かめる

goroutine の診断での確かさ:

- 主因
  - trace のチャンクの状態の変化から決めたら FACT。goroutine のスナップショットのスタックから推定した待機理由だけで決めたら INFERENCE
- ログと goroutine の結びつき
  - ロガーアダプタが振った番号で結んだら FACT。その時刻に走っていた goroutine が 1 つだけだったら INFERENCE
- goroutine ごとのリーク
  - 同じスタックの goroutine がちょうどリークの件数だけあったら FACT。待っている時間が長い順に件数分を選んだら INFERENCE

診断には「何から算出したか」が付く。細かい順に、`execution-trace` (期間をそのまま切り出せる)・`goroutine-state-breakdown` (1 分ごとの合計。期間は 1 分単位に合わせてある)・`goroutine-snapshot` (その瞬間の状態だけ)。
