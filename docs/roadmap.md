# gorviz ロードマップ

[design.md](design.md) に対する進み具合。設計より頻繁に変わるので、ここに分けて持つ。

## できていること (ドラフト)

- 録画 (常時 trace)、goroutine dump、ログの取り込み。dump は `-dump-interval` (既定 1 分) ごとで、画面の更新 (`-refresh`) とは別
- slogtrace によるログと goroutine の結びつけ
- GMP の概念図での再生 (Scheduler と、ログからの再生)。呼び方は公式の名前に合わせ、P の syscall 状態はなくした
- 画面: Overview・Issues・Logs・Scheduler・Goroutine。G をクリックすると Goroutine に移る
- Goroutine の診断: 状態別の時間内訳と主因 (録画の区切りをまたいで合計)、主因の状態で止まっていた場所 (file:line)、GC (STW / mark assist) の取り込み
- Issue の検出: `LEAK`・`ERROR_SPIKE`・`SLOW_REQUEST`・`CPU_WAIT_SPIKE`・`MEMORY_PRESSURE` (GC の頻度で判定)。確かさ `FACT` / `INFERENCE` を付ける
- ログの所要時間の読み取りと、リクエストの区間での Goroutine の表示
- ローカルでの起動 (`gorviz run` / `serve` / `top`。今は `cmd/gorviz` 1 つで、1 プロセスが agent と server を兼ねる)

### 段階 1・2 の残り

- `CGO` の判定 (trace でシステムコールと見分けられるか未確認)
- 時刻からのログと G の結びつき (`INFERENCE`)。今は slogtrace の番号がないログは G を出さない
- `MEMORY_PRESSURE` のヒープの張り付き (GOMEMLIMIT) による判定。今は GC の頻度だけ
- TUI で GUI とおおむね同じ画面を見られるようにする (今は Scheduler と Overview の一部だけ)
- Goroutine から「その G を含む Issue」「前後のログ (時刻で絞った Logs)」に戻る導線

## 実測

`go run ./example/overhead` (Apple Silicon、GOMAXPROCS=8、2026-11-15) の結果。CPU・channel・割り当て・ロックを混ぜた仕事を trace なし / ありで交互に回した。

- execution trace を取り続けたときの処理数の低下: 平均 2.4% (回ごとに -1〜6% とばらつく)。目安の 1〜2% より少し大きいが同程度
- goroutine dump (`debug=2`) にかかる時間: 待機中の G が 1,000 で 2.5ms、10,000 で 70ms、100,000 で 780ms。G 1 つあたり約 7µs で、Datadog の目安 (約 1µs) より重い。`debug=2` は全 G のスタックを文字にする間ずっと世界を止めるため。G が多い対象では `-dump-interval` を長くするか、軽い形式 (`debug=1` や proto) に切り替える必要がある (未着手)

## 未着手

上から順に進める。各段階の終わりに、その段階でできるようになることを確かめる。

### 1. ローカルで Goroutine の診断が出る / 2. Issues と Overview で問題に気づける

PoC で大部分ができた。残りは上の「段階 1・2 の残り」。

### 3. アプリに負荷をかけずに常に録れる

- 常時 trace のオーバーヘッドの実測 (Datadog の Go プロファイラと同程度か)
- 録画をバイト数の上限つきのリングバッファで持つ仕組み
- 録り方の切り替え (常時 / 間欠) と、dump・リーク判定の間隔の設定

### 4. agent と server に分ける

- 入口を別バイナリに分ける (`cmd/agent` / `cmd/server` / `cmd/tui` / `cmd/cli` / `cmd/mcp`)
- agent から server へ外向きにつなぎ、やり取りは server からの pull にする (軽いものは数秒ごと、trace は必要なときだけ)
- server によるきっかけの判定と、前後の録画の保存
- GUI・TUI・CLI・MCP は server とだけ話す (今の TUI が対象を直接読んでいるなら server 経由に直す)
- CLI でローカルの agent と server をまとめて起動する

### 5. 本番に置ける

- daemon 型の配置 (VM、ECS on EC2、Kubernetes)
- sidecar 型 (Fargate、Cloud Run)
- ライブラリ型 (FlightRecorder)

### 6. 広げる

- LLM 向けの口 (MCP)
- goroutine を代わりに起動するライブラリ (kakkky/scope、errgroup など) のラッパーを飛ばしたラベル付け
- kakkky/scope のスコープの可視化
