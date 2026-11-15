# gorviz

Go ランタイムを可視化するツール。G / M / P の対応と goroutine リークを、ターミナル (`gorviz top`) とブラウザ (`gorviz serve`) で見る。

何のためのツールか (What / Why / How) は [docs/overview.md](docs/overview.md) を参照。

## 使い方

観測対象は `net/http/pprof` を公開しておく:

```go
import _ "net/http/pprof"

go http.ListenAndServe("localhost:6060", nil)
```

```sh
gorviz top   -target http://localhost:6060
gorviz serve -target http://localhost:6060 -addr localhost:7070

# 本番の場合は pprof のポートを手元に転送して使う
kubectl port-forward pod/api-xxx 6060 && gorviz serve -target http://localhost:6060

# ローカルでは対象を gorviz から起動すると、runtime が出す run queue の長さも並べて見られる
gorviz run -- ./myapp --flag
gorviz run -ui top -- go run ./cmd/myapp
```

試すだけなら `./bin/gorviz run -- go run ./example/leaky` で、リークし続けるデモアプリを gorviz の下で起動できる。

## 画面

- **Live** (`/`): 最新の観測窓を GMP の概念図で再生し続ける。リーク判定・G の推移・P ごとのタイムラインもここ
- **Explore** (`/explore`): 対象のログを時間軸のヒストグラムと一覧で見る。1 行クリックすると、その瞬間の前後 (±50ms / ±250ms / ±1s) を録画から切り出してスロー再生し、ログの瞬間で一度止まる

ログの取り込み元:

- `gorviz run -- ./app`: 子プロセスの stdout / stderr
- `kubectl logs -f pod | gorviz serve -logs -`: 標準入力

ログの時刻は slog (JSON / テキスト) と標準 log の形式から読む。対象の slog を `slogtrace` で包むと、ログ 1 行ごとに trace にも印が残り、**どの goroutine がそのログを書いたか**まで特定して再生中に青い輪で示す:

```go
logger := slog.New(slogtrace.Wrap(slog.NewJSONHandler(os.Stderr, nil)))
```

## 何を見ているか

裏で録画ループが動き、画面はそれが貯めたものを読む (trace は対象のプロセスで同時に 1 本しか取れないため):

- `/debug/pprof/trace?seconds=<chunk>` — `-chunk` (既定 2 秒) ごとの execution trace を間を空けずに取り続け、`-retention` (既定 5 分) 分を保持する。trace は常時オンになる
- 以下は `-interval` ごとに取る
- `/debug/pprof/goroutine?debug=2` — 全 G の状態・待機時間・生成元 (`created by`)
- `/debug/pprof/goroutineleak?debug=2` — Go 1.26+ の GC ベースのリーク検査。到達不能な G に `(leaked)` が付く。対象で GC が走るので `-leak-check-every` 回に 1 回だけ取る

GUI の「GMP replay」は、trace の観測窓を時間軸で再生し、GRQ / LRQ / P / M / 待機中の G (sudog など) / 終了した G (gFree) / pidle の概念図として描く。
LRQ / GRQ の中身は runtime が外に出さないので、trace から推測している:

- 生成・unblock された G は、それを行った P の LRQ に積まれる (P を持たない文脈なら GRQ)
- 横取り (preempt) や Gosched で降ろされた G、syscall から戻ったときに P を取れなかった G は GRQ に積まれる
- runnext (LRQ の先頭に割り込む枠) は区別しない

`gorviz run` は対象を `GODEBUG=schedtrace=1000,scheddetail=1` で起動し、runtime 自身が出す GRQ の長さ・P ごとの LRQ の長さ・gFree の数を表示する。推測と突き合わせるためのもので、対象を gorviz の子プロセスとして起動し直す必要があるためローカル専用。

リークの判定には 2 種類ある:

- **LEAKED**: runtime が到達不能と判定した G。誤検知がない
- **SUSPECT**: 生成元ごとの待機中の G の数が、直近の観測で一度も減らずに `-leak-min-growth` 以上増えたもの。goroutineleak を持たない古い Go 向けの推測

## 構成

[kakkky.dev](https://github.com/kakkky/kakkky.dev) のレイヤー構成に倣う。依存は外側から内側への一方向。

```
cmd/gorviz           エントリポイント (top / serve / run の振り分け、Wire injector)
driver/
  httpserver         HTTP サーバ・mux
  recorder           録画ループ (trace の区切り / goroutine dump のスナップショット)
  loginput           対象のログを 1 行ずつ取り込む
  childprocess       gorviz run で対象を schedtrace つきで起動する
presentation/
  handler            GUI の HTTP ハンドラ (ダッシュボード / SSE)
  terminal           CLI の描画ループ
  view               Templ (layout / pages / partials / components)
assets/dist/js       GMP replay を描く Stimulus controller
usecase              観測 1 回分のスナップショット組み立て・リーク検出の履歴管理
domain               G / P / M のモデル、リーク判定、RuntimeClient interface
adapter/
  client             pprof を叩く RuntimeClient、goroutine dump と execution trace のパーサ
  kvs                観測履歴を置くインメモリ KVS
  schedtrace         子プロセスの標準エラー出力から SCHED ブロックを読む
  recording          録画した trace とログの保持 (保持期間・上限つき)
  logparse           ログ 1 行から時刻・レベル・メッセージを読む
slogtrace            観測対象が import する slog ハンドラ (ログと trace を番号で結ぶ)
```

コードを生成し直すとき: `make gen` (templ + wire)
