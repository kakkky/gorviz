# Issue の種類・判定の線・ネクストアクション

Issue は runtime 層の問題。解決の状態は持たず、どこかのインスタンスで発生が続いていれば発生中。数える単位は関数 (無名関数は外側の関数にまとめる)。

- `GOROUTINE_LEAK`
  - 終わるはずの goroutine が溜まり続ける
  - FACT
    - runtime の goroutineleak プロファイルが判定した
  - INFERENCE
    - 生成元ごとの待機中の数が減らずに増え続けている
  - 次の一手
    - 生成元の go 文と止まっている場所を見て、終われる道 (context のキャンセル・channel を閉じる・タイムアウト) があるかを見直す
- `CPU_CONTENTION`
  - `RUNNABLE` の goroutine が P を待たされる
  - INFERENCE
    - `RUNNABLE` から `RUNNING` までの p99 が 1ms 以上、または普段より大きく外れた
  - 次の一手
    - 同じ時間帯に CPU を長く使っている goroutine か、CPU の割り当てを見直す
- `GC_PRESSURE`
  - GC が CPU を奪う
  - FACT
    - GC の CPU の割合が 50% に達した
  - INFERENCE
    - mark assist の CPU の割合が 5% 超、または GC の CPU の割合が普段より外れた
  - 次の一手
    - mark assist の長い goroutine の割り当てを減らす。GOGC・GOMEMLIMIT を見直す
- `LOCK_CONTENTION`
  - 仕事より長い時間をロックの待ちに取られる
  - INFERENCE
    - ロックで待たされた時間が実行中の時間を超えた、または普段より外れた (インスタンス全体で判定)
  - 次の一手
    - 待ちの多かった `Lock` を呼んだ関数を見る。回数が多いのか、誰かが長く持っているのかを、回数と 1 回あたりの長さで見分け、待たせていた関数の goroutine をたどる
- `LONG_STW`
  - stop-the-world でアプリ全体が長く止まる
  - INFERENCE
    - 1 回の STW が 1ms 以上
  - 次の一手
    - STW の理由を見て、`runtime.ReadMemStats`・`runtime.GC`・プロファイルの取得などの呼び出しを見直す
- `THREAD_EXPLOSION`
  - OS のスレッドが増えすぎる
  - INFERENCE
    - スレッドの数が上限 (既定 10,000) の半分を超えた、または普段より外れた
  - 次の一手
    - syscall・cgo の場所を見て、同時に呼ぶ数を絞る

「普段より外れた」は、同じ曜日・同じ時刻の前後 30 分の 5 分ごとの値 (直近 3 週まで) の中央値と MAD からの修正 z スコアが 3.5 を超えたこと。
