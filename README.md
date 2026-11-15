# gorviz

Go アプリの goroutine のリークと、遅い / 失敗したリクエストの原因を調べるツール。問題の兆候やログ 1 行から、その瞬間に goroutine が何に止まっていたかを診断し、根拠 (状態別の時間内訳と GMP の再生) を見せる。

何のためのツールか (What / Why / How) は [docs/overview.md](docs/overview.md)、設計は [docs/design.md](docs/design.md) を参照。

## 構成

用途ごとに別のバイナリにしている。`make build` で `bin/` に作る。

- `gorviz-agent`: 対象の隣に置き、trace・goroutine dump・ログを取ってリングバッファに持つ。server へ外向きにつなぎ、server に求められたら返す。画面は持たない
- `gorviz-server`: agent を束ね、診断・Issue の検出・Issue の前後の録画の保存と、GUI・JSON の API を受け持つ
- `gorviz-tui`: GUI とおおむね同じ画面を端末で見る (server の API とだけ話す)
- `gorviz` (CLI): ローカルで server と agent をまとめて起動する (`up`)、Issue を表示する (`issues`)、保存した録画を書き出す (`export`)
- `gorviz-mcp`: LLM エージェント向けの MCP サーバ (stdio。server の API とだけ話す)
- `gorvizagent` (ライブラリ): アプリに埋め込む agent。runtime の FlightRecorder で直近を持つ

```
gorviz-agent ──(agent から外向きの TCP。server が pull する)──> gorviz-server <── GUI / gorviz-tui / gorviz / gorviz-mcp
```

## 使い方

観測対象は `net/http/pprof` を公開しておく:

```go
import _ "net/http/pprof"

go http.ListenAndServe("localhost:6060", nil)
```

```sh
make build

# ローカル: server と agent をまとめて起動する。-- の後ろを付けると、agent が対象を起動し、runtime が出す run queue の長さも読む
bin/gorviz up -target http://localhost:6060
bin/gorviz up -save-dir ./recordings -- ./myapp --flag

# 別々に起動する (agent はいくつでもつなげる。daemon 型は -target をカンマで並べる)
bin/gorviz-server -addr localhost:7070 -agent-addr localhost:7071 -save-dir ./recordings
bin/gorviz-agent -name api -target http://localhost:6060 -agent-addr localhost:7071

# 見る
open http://localhost:7070
bin/gorviz-tui -server http://localhost:7070
bin/gorviz issues
bin/gorviz export -out ./recordings-export   # go tool trace で開ける
```

試すだけなら `make up` で、リークし続け、ときどきロック待ちで遅いリクエストを出すデモアプリを gorviz の下で起動できる。ライブラリ型は `make example.library` (gorviz-server を先に起動しておく)。

MCP は、Claude Code などの設定に次のように足す:

```json
{"mcpServers": {"gorviz": {"command": "/path/to/bin/gorviz-mcp", "args": ["-server", "http://localhost:7070"]}}}
```

ツールは `list_agents` / `get_overview` / `list_issues` / `diagnose_goroutine` / `search_logs` / `list_recordings` で、読み取りだけ。

## 画面

- **Overview** (`/`): 全体の様子。未解決の Issue の数、goroutine 数の推移、直近 1 分のエラーと遅いリクエストの数、P を待つ G の数、ヒープと GC の目標値、GC の頻度
- **Issues** (`/issues`): 検出した問題の一覧。種類 (`LEAK` / `ERROR_SPIKE` / `SLOW_REQUEST` / `CPU_WAIT_SPIKE` / `MEMORY_PRESSURE`) と確かさ (`FACT` / `INFERENCE`) のラベルを付け、代表の G の Goroutine や絞り込んだ Logs に進める
- **Logs** (`/logs`): 対象のログを時間軸のヒストグラムと一覧で見る。1 行クリックすると、その瞬間の前後 (±50ms / ±250ms / ±1s) を録画から切り出してスロー再生し、ログの瞬間で一度止まる。ログを書いた G が分かれば、その G の Goroutine に進める
- **Scheduler** (`/scheduler`、見出しは Runtime Scheduler): 最新の観測窓を GMP の概念図で再生し続ける。G をクリックすると、その時点の Goroutine に移る
- **Goroutine** (`/goroutines/{id}?at=&from=`): 1 つの G の診断。主因 (`RUNNING` / `CPU_WAIT` / `BLOCKED` / `IO_WAIT` / `CGO` / `SLEEP` / `GC`) と止まっていた場所 (file:line) を出し、根拠として状態別の時間内訳と GMP の再生を見せる。所要時間のあるログから来たときは、そのリクエストの区間 (書いた時刻 − 所要時間 〜 書いた時刻) を見る

`CGO` は、trace では syscall として記録されるので、スタックに `runtime.cgocall` があるかで見分ける。

画面の右上の **Agents** で、どの agent (対象) を見るかを選ぶ。Issues の下には、Issue をきっかけに server が保存した前後の録画が並び、`go tool trace` で開ける形で落とせる。

ログの取り込み元 (agent):

- `gorviz-agent -- ./app` (または `gorviz up -- ./app`): 子プロセスの stdout / stderr
- `kubectl logs -f pod | gorviz-agent -logs -`: 標準入力
- ライブラリ型: `gorvizagent.Agent.LogWriter()` に書いたログ

ログの時刻は slog (JSON / テキスト) と標準 log の形式から読む。`-duration-keys` (既定 `duration,latency,elapsed`) のキーがあれば所要時間として読み、`-slow-request` (既定 1 秒) を超えたものを遅いリクエストとみなす。JSON の数値は slog の既定と同じくナノ秒、文字列は `1.2s` の形で読む。対象の slog を `slogtrace` で包むと、ログ 1 行ごとに trace にも印が残り、**どの goroutine がそのログを書いたか**まで特定して再生中に青い輪で示す:

```go
logger := slog.New(slogtrace.Wrap(slog.NewJSONHandler(os.Stderr, nil)))
```

## 何を見ているか

agent の録画ループが対象から取り、server は agent に求めて読む (trace は対象のプロセスで同時に 1 本しか取れないため、1 つの対象を観測する agent は 1 つ):

- `/debug/pprof/trace?seconds=<chunk>` — `-chunk` (既定 2 秒) ごとの execution trace を間を空けずに取り続け、`-trace-buffer-mib` (既定 256 MiB) のリングバッファに直近を持つ。上限は時間ではなくバイト数なので、対象が忙しいほど持てる時間は短くなる (Overview に今持てている区間を出す)
- `/debug/pprof/goroutine?debug=2` — `-dump-interval` (既定 1 分) ごとに取る。全 G の状態・待機時間・生成元 (`created by`)。dump は対象の全 G を止める (STW) ので間を空ける
- `/debug/pprof/goroutineleak?debug=2` — Go 1.26+ の GC ベースのリーク検査。到達不能な G に `(leaked)` が付く。対象で GC が走るので、dump `-leak-check-every` 回に 1 回だけ取る
- server は `-refresh` (既定 2 秒) ごとに、agent から軽いもの (ログ・dump・最新の区切り) を取ってきて判定する。trace の生のデータは、Issue の前後を保存するときと画面で見るときだけ運ぶ
- `-record-mode intermittent` (`-record-every` ごとに `-record-for` だけ録る) にすると負荷の総量は減るが、問題の瞬間を録り逃すことがある

GUI の「GMP replay」は、trace の観測窓を時間軸で再生し、global run queue / local run queue / P / M / 待機中の G (sudog など) / free G list / idle P list の概念図として描く。呼び方は Go の runtime の公式の名前に合わせている (LRQ / GRQ などの略称は使わない)。P にはシステムコール中の状態がないので、システムコールは G の状態 (`_Gsyscall`) で描く。
run queue の中身は runtime が外に出さないので、trace から推測している:

- 生成・unblock された G は、それを行った P の local run queue に積まれる (P を持たない文脈なら global run queue)
- 横取り (preempt) や Gosched で降ろされた G、syscall から戻ったときに P を取れなかった G は global run queue に積まれる
- runnext (local run queue より先に実行される 1 枠) は区別しない

`gorviz-agent -- cmd` は対象を `GODEBUG=schedtrace=1000,scheddetail=1` で起動し、runtime 自身が出す global run queue の長さ・P ごとの local run queue の長さ・free G の数を表示する。推測と突き合わせるためのもので、対象を agent の子プロセスとして起動し直す必要があるためローカル専用。

リーク (`LEAK`) の確かさは 2 種類ある:

- **FACT**: runtime が到達不能と判定した G。誤検知がない
- **INFERENCE**: 生成元ごとの待機中の G の数が、直近の dump で一度も減らずに `-leak-min-growth` 以上増えたもの。goroutineleak を持たない古い Go 向けの推測

## コードの構成

[kakkky.dev](https://github.com/kakkky/kakkky.dev) のレイヤー構成に倣う。依存は外側から内側への一方向。

```
cmd/
  agent              gorviz-agent (daemon 型は対象ごとに agent を立てる。-- cmd で対象を子プロセスとして起動)
  server             gorviz-server
  tui / cli / mcp    gorviz-tui / gorviz / gorviz-mcp (server の API のクライアント)
driver/
  agent              agent 1 つ分の組み立て (録画ループ・ログの取り込み・server へのつなぎ)。ライブラリ型も使う
  session            server の側の、agent ごとのセッション (ログの同期・診断・保存) と、agent の一覧
  httpserver         HTTP サーバと、server の入口の mux (agent の選択・API・agent ごとの画面への振り分け)
  recorder           agent の録画ループ (trace の区切り / goroutine dump)
  loginput           対象のログを 1 行ずつ取り込む
  childprocess       対象を schedtrace つきで起動する
presentation/
  handler            GUI の HTTP ハンドラと JSON の API
  terminal           TUI
  mcp                MCP サーバ
  view               Templ (layout / pages / partials / components)
api                  JSON の API の型 (server と、TUI・CLI・MCP が共有する)
assets/dist/js       GMP replay を描く Stimulus controller
usecase              agent の側 (trace・dump・ログを取る) と server の側 (診断・Issue の検出・保存) の usecase
domain               G / P / M のモデル、状態別の時間内訳と主因、Issue の検出、リーク判定、確かさ、外とのやり取りの interface
adapter/
  client             pprof を叩く RuntimeClient、goroutine dump と execution trace のパーサ
  inprocess          アプリの中で取る RuntimeClient (FlightRecorder。ライブラリ型)
  agentrpc           agent と server の間の口 (agent が外向きにつないだ TCP の上の net/rpc。server が呼び出す側)
  serverapi          server の API のクライアント
  kvs                観測履歴を置くインメモリ KVS
  schedtrace         子プロセスの標準エラー出力から SCHED ブロックを読む
  recording          録画のリングバッファ (バイト数の上限)、ログの保持、Issue の前後の録画の保存
  logparse           ログ 1 行から時刻・レベル・メッセージ・所要時間を読む
gorvizagent          アプリに埋め込む agent (ライブラリ型)
slogtrace            観測対象が import する slog ハンドラ (ログと trace を番号で結ぶ)
example/             デモの対象 (leaky / library) と負荷の実測 (overhead)
deploy/              配置の例 (Kubernetes の daemon 型・sidecar 型、ECS の sidecar 型)
```

コードを生成し直すとき: `make gen` (templ)
