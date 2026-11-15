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

## 未着手

上から順に進める。各段階の終わりに、その段階でできるようになることを確かめる。

### 1. ローカルで Goroutine の診断が出る (ジョブ 2 の中核)

- 状態別の時間内訳と、主因 (`RUNNING` / `CPU_WAIT` / `BLOCKED` / `IO_WAIT` / `CGO` / `SLEEP` / `GC`) の判定
- GC (STW / mark assist) の取り込み
- 止まっている場所の file:line の表示
- 確かさ (`FACT` / `INFERENCE`) の表示。リークの LEAKED / SUSPECT も置き換える
- 画面の部品と状態の名前を公式の呼び方に合わせる (LRQ / GRQ を local run queue / global run queue に、pidle を idle P list に、など)。P の syscall 状態 (Psyscall) は今の Go にはないので、システムコールは G の状態 (`_Gsyscall`) で描く

### 2. Issues と Overview で問題に気づける

- Overview・Issues 画面 (リークは Issues の中の `LEAK` として、他の種類と同じ形で見せる)
- 種類ごとの検出: `LEAK`・`ERROR_SPIKE`・`CPU_WAIT_SPIKE`・`MEMORY_PRESSURE`
- ログの所要時間の読み取りと、リクエストの区間での Goroutine の表示、`SLOW_REQUEST` の検出
- TUI で GUI とおおむね同じ画面を見られるようにする

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
