# 入口ごとの調べ方の流れ (リーク・ロック・エラーログ・遅いリクエスト・時間帯)

- goroutine が溜まる
  - `list-issues` で `GOROUTINE_LEAK` → `get-issue` でスタックごとの「M 個のうち N 個がリーク」と生成元を見る → `diagnose-goroutine` で待機理由とリークの確かさ
- ロックの待ち
  - `list-issues` で `LOCK_CONTENTION` → `get-issue` で `Lock` を呼んだ関数ごとの回数・平均・最大と待たせていた関数 → 待たせていた goroutine を `diagnose-goroutine` (`woken`) でたどる
- エラーログ
  - `search-logs` (`level=ERROR`) → `get-log` で書いた goroutine → `diagnose-goroutine` (`log`)。エラーログの前後の trace は長く残してある
- 遅いリクエスト
  - 所要時間のあるログを `get-log` → `diagnose-goroutine` (`log`)。期間はそのリクエストの区間になる。主因が `RUNNABLE` なら `CPU_CONTENTION`、`GC` なら `GC_PRESSURE` を同じ時間帯で探す
- APM で気づいた時間帯
  - `get-overview` (`from`・`to`) → Issue があれば上と同じ。なければ `summarize-goroutines` で生成元ごとの主因を見る。runtime に原因がなければアプリ層を APM で調べる

一覧は要点と次の ID だけを返す。中身は詳細の操作で取る。返る量で区切ってあり、続きは `cursor` で取る。
