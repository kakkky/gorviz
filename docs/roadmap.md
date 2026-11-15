# gorviz ロードマップ

[design.md](design.md) に対する進み具合。設計より頻繁に変わるので、ここに分けて持つ。

## できていること (ドラフト)

- 録画 (常時 trace)、goroutine dump、ログの取り込み
- slogtrace によるログと goroutine の結びつけ
- GMP の概念図での再生 (Scheduler と、ログからの再生)
- リーク判定 (今の表示は LEAKED / SUSPECT。`FACT` / `INFERENCE` への置き換えは未着手)
- ローカルでの起動 (`gorviz run` / `serve` / `top`)

## 未着手 (ジョブに必要な順)

1. Goroutine 画面: 状態別の時間内訳と、主因の判定 (ジョブ 2 の中核)
2. 止まっている場所の file:line の表示
3. GC (STW / mark assist) の取り込み
4. Overview・Issues 画面
5. `MEMORY_PRESSURE` の検出
6. ログの所要時間の読み取りと、リクエストの区間での Goroutine の表示、`SLOW_REQUEST` の判定
7. 常時 trace のオーバーヘッドの実測 (Datadog の Go プロファイラと同程度か)
8. 録り方の切り替え (常時 / 間欠) と、dump・リーク判定の間隔の設定
9. 録画をバイト数の上限つきのリングバッファで持つ仕組み
10. gorviz-agent / gorviz-server への分離と、daemon 型の配置 (VM、ECS on EC2、Kubernetes)
11. server からの pull (軽いものを数秒ごと、trace は必要なときだけ) と、server によるきっかけの判定と前後の録画の保存
12. sidecar 型 (Fargate、Cloud Run)
13. ライブラリ型 (FlightRecorder)
14. goroutine を代わりに起動するライブラリ (kakkky/scope、errgroup など) のラッパーを飛ばしたラベル付け
15. kakkky/scope のスコープの可視化
16. 入口を別バイナリに分ける (`cmd/agent` / `cmd/server` / `cmd/tui` / `cmd/cli` / `cmd/mcp`)。今は `cmd/gorviz` 1 つで top / serve / run を振り分けている
17. LLM 向けの口 (MCP)
18. 画面の部品と状態の名前を公式の呼び方に合わせる (LRQ / GRQ を local run queue / global run queue に、pidle を idle P list に、など)。P の syscall 状態 (Psyscall) は今の Go にはないので、システムコールは G の状態 (`_Gsyscall`) で描く

## 配置の対応順

1. ローカル (現状)
2. daemon 型: VM、ECS on EC2、Kubernetes
3. sidecar 型: Fargate、Cloud Run (録画を gorviz-server へ送る仕組みが前提)
4. ライブラリ型
