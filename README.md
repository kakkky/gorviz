# gorviz

**見えにくい Go ランタイムのスケジューリングを可視化し、goroutine が絡む問題の調査に使うツール。**

goroutine のリークや、遅い / 失敗したリクエストの原因は、たいてい「その goroutine がランタイムの中で何を待っていたか」にある。ただ、スケジューラの動きはふだん見えず、`go tool trace` を読み解くにはランタイムの知識が要る。gorviz はそれを、開発者がそのまま読める形で見せる。

- **スケジューリングを見せる**: goroutine を P と M がどう走らせ、どこで待たせたかを GMP の概念図で再生する
- **何に止まっていたかを言う**: CPU 待ち・ロック・I/O・GC のどれだったかを、状態別の時間内訳と、止まっていた場所 (file:line) で示す
- **問題の瞬間から入れる**: リークの兆候やログ 1 行から、該当の goroutine のその瞬間に辿れる。trace を常に取り続け、問題の前後は保存するので、気づいてから取り直さなくてよい

仕様と設計は [docs/design.md](docs/design.md) を参照。

## 構成

```
対象アプリ <── gorviz-agent ──(外向き TCP)──> gorviz-server <── ブラウザ / gorviz / gorviz-mcp
```

- `gorviz-agent`: 対象の隣で trace・goroutine dump・ログを取り、直近を持つ
- `gorviz-server`: agent を束ね、診断・Issue の検出・前後の trace の保存と、GUI・API を受け持つ
- `gorviz` (CLI) / `gorviz-mcp`: server の API のクライアント
- `gorviz-launcher`: ローカルで server と agent をまとめて起動する
- `github.com/kakkky/gorviz`: アプリが import するライブラリ (任意)

## クイックスタート

```sh
make launcher.up
open http://localhost:8080   # デモの Todo アプリ
open http://localhost:7070   # gorviz
```

デモの Todo アプリ (`example/todo`) を gorviz の server と agent で観測する。Todo アプリには、画面の操作で起きる goroutine の不具合が入っている:

- リマインダー付きで追加する → 通知のタイムアウトで goroutine が残り続ける (`LEAK`)
- 今すぐバックアップ (1 分ごとにも自動で走る) → その間のリクエストがロック待ちで遅くなる (ログから入り、待たされた G が `BLOCKED` と分かる)
- 完了にする → 2 分ごとに 30 秒、メール送信が失敗する (ログから入り、G は外の呼び出しを待っているだけと分かる)
- サンプルを追加してから統計を見る → P が埋まり、他のリクエストが実行を待たされる (`CPU_WAIT_SPIKE`)
- サンプルを追加してから CSV エクスポートを繰り返す → GC が頻発する (`MEMORY_PRESSURE`)

裏では仮想ユーザー (`-users`、既定 3) が一覧・追加・完了を続ける。

## 使い方

### 1. 対象を準備する

次のどちらか。

- **pprof を公開する** (agent を隣に置く場合)

  ```go
  import _ "net/http/pprof"

  go http.ListenAndServe("localhost:6060", nil)
  ```

- **agent をアプリに埋め込む** (agent を置けない環境。pprof の公開は要らない)

  ```go
  a, err := gorviz.Start(ctx, gorviz.Options{Server: "gorviz-server:7071"})
  defer a.Stop()
  ```

どちらの場合も、ロガーに gorviz を差し込むと、ログ 1 行とそれを書いた goroutine を確実に結べる。埋め込んだ agent には、ログもそのまま渡る。

```go
// slog
logger := slog.New(gorviz.NewSlogHandler(slog.NewJSONHandler(os.Stderr, nil)))

// zap (github.com/kakkky/gorviz/logger/zapadapter)
core := zapcore.NewCore(enc, zapadapter.WrapWriteSyncer(zapcore.AddSync(os.Stderr)), zap.InfoLevel)
logger := zap.New(zapadapter.WrapCore(core))

// zerolog (github.com/kakkky/gorviz/logger/zerologadapter)
logger := zerolog.New(zerologadapter.WrapWriter(os.Stderr)).Hook(zerologadapter.Hook())

// logrus (github.com/kakkky/gorviz/logger/logrusadapter)
logger.SetFormatter(logrusadapter.WrapFormatter(&logrus.JSONFormatter{}))
```

ログの書式は変わらない。1 行ごとに `gorviz_seq` (trace と結ぶ番号) のフィールドが 1 つ足される。

### 2. 起動する

```sh
make build

# ローカル: server と agent をまとめて起動
bin/gorviz-launcher -target http://localhost:6060 -- go run ./myapp   # 対象も起動し、その出力をログとして取り込む
bin/gorviz-launcher -target http://localhost:6060                     # すでに動いている対象を見る (ログは取り込まない)

# 別々に起動 (本番の形)
bin/gorviz-server -addr localhost:7070 -agent-addr localhost:7071 -save-dir ./saved
bin/gorviz-agent  -name api -target http://localhost:6060 -agent-addr localhost:7071
```

- 1 つの agent で複数の対象を見るときは `-target` をカンマで並べる
- Kubernetes・ECS・Cloud Run での設定例は [deploy/](deploy/README.md)

### 3. 見る

```sh
open http://localhost:7070                  # GUI
bin/gorviz issues                           # Issue の一覧
bin/gorviz export -out ./export             # 保存した trace を go tool trace で開ける形で書き出す
```

GUI の画面:

- **Overview**: runtime の様子の推移 (未解決の Issue、goroutine 数と状態・ヒープ・GC・mark assist・CPU 待ち)。期間を選んで見る
- **Issues**: 検出した runtime の問題 (`LEAK` / `CPU_WAIT_SPIKE` / `MEMORY_PRESSURE`) と、保存した前後の trace
- **Logs**: ログ一覧。1 行選ぶと、その瞬間の前後を再生し、書いた goroutine に進める
- **Scheduler**: 最新の録画の末尾を GMP の概念図で再生し続ける。再生する長さ (100ms〜2s) をボタンで選び、P ごとの帯 (CPU slots (P) over time) で G が走った瞬間を選んで飛べる
- **Goroutine**: 1 つの goroutine の主因・止まっていた場所 (file:line)・根拠
- **Agents** (右上): 見る対象を選ぶ

### LLM エージェントから使う

Claude Code などの MCP の設定に足す。

```json
{"mcpServers": {"gorviz": {"command": "/path/to/bin/gorviz-mcp", "args": ["-server", "http://localhost:7070"]}}}
```

ツールは `list_agents` / `get_overview` / `list_issues` / `diagnose_goroutine` / `search_logs` / `list_recordings`。読み取りだけ。

## ログの取り込み

取り込み元:

- `gorviz-launcher -- ./app`: launcher が起動した対象の標準出力・標準エラー出力
- `./app 2>&1 | gorviz-launcher -logs -` / `kubectl logs -f pod | gorviz-agent -logs -`: 標準入力
- ライブラリ型: 上のロガーの差し込み口から

読み取り:

- 書式: slog・zap・zerolog・logrus (JSON / テキスト) と標準 log の形式
- 所要時間: `-duration-keys` (既定 `duration,latency,elapsed`) のキー。数値はナノ秒、文字列は `1.2s` の形。ログを選んだときに、そのリクエストの区間を再生するのに使う

遅いリクエストやエラーの増加に気づくのは APM (Datadog など) の役目とし、gorviz はそこで見つけたログ・時間帯から、goroutine と runtime の中で何が起きていたかを調べる入口にする。

## 主なフラグ

agent と server はそれぞれ自分の flag だけを受け付ける。launcher は `-addr` / `-agent-addr` / `-save-dir` を server に渡し、残りをすべて agent に渡す。`--` より後ろは対象を起動するコマンド。

- **取得 (gorviz-agent)**
  - `-chunk` (既定 2s): trace を区切る長さ
  - `-trace-buffer-mib` (既定 256): trace の直近を持つ上限。対象が忙しいほど持てる時間は短い
  - `-dump-interval` (既定 1m): goroutine dump の間隔。dump は全 goroutine を一瞬止める
  - `-leak-check-every` (既定 5): dump 何回に 1 回、runtime のリーク判定を行うか (GC を伴う。0 で無効)
  - `-record-mode` (既定 `always`): `intermittent` にすると `-record-every` ごとに `-record-for` だけ取る。負荷は減るが、問題の瞬間を取り逃すことがある
  - `-refresh` (既定 2s): agent が server へ送る間隔
- **判定 (gorviz-server)**
  - `-cpu-wait` / `-gc-per-second`: Issue の閾値
  - `-leak-min-growth`: 件数の推移でリークとみなす増加数
- **保存 (gorviz-server)**
  - `-save-dir`: Issue の前後の trace を保存する先 (空ならメモリだけ)

一覧は `-h` で見る。

## 開発

```sh
make build            # bin/ にすべてのバイナリを作る
make test
make gen              # templ のコードを生成し直す
make example.todo     # デモの Todo アプリを単体で起動 (http://localhost:8080、pprof は localhost:6060)
make example.library  # ライブラリ型のデモ (gorviz-server を先に起動しておく)
make overhead         # gorviz が対象にかける負荷を測る
```
