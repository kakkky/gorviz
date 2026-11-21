# gorviz 設計

用語の説明は脚注にまとめた。本文の番号 (`[1]` など) から飛べる。

## 目次

- [概要](#概要)
- [モチベーション](#モチベーション)
- [アーキテクチャ設計](#アーキテクチャ設計)
  - [コンポーネント](#コンポーネント)
  - [依存関係](#依存関係)
  - [データフロー](#データフロー)
- [コンポーネント仕様](#コンポーネント仕様)
  - [gorviz-agent](#gorviz-agent)
  - [gorviz-server](#gorviz-server)
- [クライアントツール仕様](#クライアントツール仕様)
  - [gorviz-cli](#gorviz-cli)
  - [gorviz-mcp](#gorviz-mcp)
- [gorviz package](#gorviz-package)
- [gorviz-launcher](#gorviz-launcher)
- [ユーザーストーリー](#ユーザーストーリー)

## 概要

- Go のサーバアプリを作り運用する開発者が、パフォーマンスやバグの原因を Go ランタイムまで降りて調べるためのツール
- 問題の検知・ログ・時間帯を入口として、goroutine の粒度での診断を提供する。当時の Go ランタイムのスケジューラの動きを根拠として見せるため、開発者は事象を視覚的に理解できる

## モチベーション

### 課題

- バグやパフォーマンス低下の原因は、アプリのコードやログを見ても分からず、Go ランタイムまで掘り下げないと辿り着けないことがある (goroutine が CPU・ロック・I/O・GC のどれを待っていたか、など)。ところが Go で開発する人でも、ランタイムのスケジューラまで理解している人はそう多くない。そのため調査が難航する。

- ランタイムの動きは execution trace[^trace] や goroutine dump[^dump] で見られるが、それらを使って効果的に調査するのは難しく、うまく扱えないことが多い。また、それらはその場で取るものであり、定常的に集めておく仕組みがない。そのため、後日に調べようとしても、問題が起きた瞬間のデータが残っていない。

### アプローチ

- ランタイムを理解していなくても読める形で見せる
  - trace の生データをそのまま見せるのではなく、goroutine ごとの診断と、Go ランタイムのスケジューラの動きのアニメーションとして見せる
- trace と goroutine dump を、調査に使える形で集めておく
  - 定常的に取り続け、問題が検知された前後の時間帯のものを一定期間残しておく。後日でもその瞬間を調べられるようにする
  - 問題の検知・ログ・時間帯を入口に、該当する goroutine に辿れるようにする

### 既存ツールとの差異

| ツール | できること | 残る課題 |
| --- | --- | --- |
| [pprof](https://pkg.go.dev/net/http/pprof) | goroutine の総数と goroutine dump を取る | - その場で取るだけで、定常的には残らない<br>- どの go 文の goroutine がリーク[^leak]しているかは、goroutine dump を手で読み解かないと分からない |
| [LeakProf](https://www.uber.com/en-US/blog/leakprof-featherlight-in-production-goroutine-leak-detection/) / [panicparse](https://github.com/maruel/panicparse) | goroutine dump を集計する | 止まっている場所の集計まで。ランタイムの中で何を待っていたかを読み解くのは利用者に残る |
| [`go tool trace`](https://pkg.go.dev/cmd/trace) / [gotraceui](https://gotraceui.dev/) | execution trace を開いて、スケジューリングまで見分けられる | - 生データを読み解くにはランタイムの知識が要る<br>- 定常的に取る仕組みがなく、問題が起きた瞬間が残らない<br>- 問題の検知・ログ・時間帯から、該当する goroutine に辿れない |

## アーキテクチャ設計

### コンポーネント

- gorviz-agent
  - 観測するアプリと同じホスト (または同じタスク) で動き、そのアプリから execution trace・goroutine dump・ログを取り、整形して server へ送る
- gorviz-server
  - agent から届いたデータを持ち、問題の検知と goroutine ごとの診断を行う。結果を GUI (Web) と HTTP API で提供する
- gorviz-cli
  - 人間が端末から server の API を使う
- gorviz-mcp
  - LLM エージェントが MCP で server の API を使う

### 依存関係

矢印は「接続を張る / 呼び出す側 → 相手」。

#### 全体図

```mermaid
flowchart LR
    subgraph appenv["アプリの環境"]
        app["アプリ"]
        agent["agent<br/>(gorviz-agent / gorviz package)"]
    end
    server["gorviz-server"]
    disk[("ローカルディスク")]
    browser["ブラウザ (GUI)"]
    cli["gorviz-cli"]
    mcp["gorviz-mcp"]
    llm["LLM エージェント"]

    agent -->|"execution trace・goroutine dump・ログを取る"| app
    agent -->|"TCP 1 本 (gRPC)"| server
    server -->|"データを保存"| disk
    browser -->|"HTTP"| server
    cli -->|"HTTP API"| server
    mcp -->|"HTTP API"| server
    llm -->|"stdio"| mcp
```

#### agent とアプリ

agent をアプリに対してどう配置するかを 3 通り用意する。

| | agent | 観測するアプリ | 具体環境例 |
| --- | --- | --- | --- |
| daemon 型 | gorviz-agent | 同じホストの複数のアプリ | VM、ECS on EC2、Kubernetes |
| sidecar 型 | gorviz-agent | 同じタスク内のアプリ 1 つ | Fargate、Cloud Run |
| ライブラリ型 | gorviz package (agent の埋め込み) | そのアプリ自身 | agent を置けない環境 |

##### agent を別プロセスにする場合 (daemon 型・sidecar 型)

```mermaid
flowchart LR
    subgraph appproc["アプリのプロセス"]
        app["アプリのコード"]
        pprof["net/http/pprof"]
    end
    subgraph agentproc["gorviz-agent のプロセス"]
        agent["agent"]
    end
    server["gorviz-server"]

    agent -->|"HTTP で trace・goroutine dump を取得"| pprof
    app -->|"ログ (ファイル / TCP・UDP / journald)"| agent
    agent -->|"TCP 1 本 (gRPC)"| server
```

- execution trace・goroutine dump を、アプリが公開する pprof の HTTP から取る
- ログを、置き方に応じた口から取る (下の「[ログの取り方](#ログの取り方)」)

##### agent をアプリに埋め込む場合 (ライブラリ型)

```mermaid
flowchart LR
    subgraph appproc["アプリのプロセス"]
        app["アプリのコード"]
        h["ロガーアダプタ"]
        subgraph lib["gorviz package (agent の埋め込み)"]
            agent["agent"]
            fr["runtime の FlightRecorder"]
        end
    end
    server["gorviz-server"]

    app -->|"ログを書く"| h
    h -->|"ログを渡す (プロセス内)"| agent
    agent -->|"trace を切り出す"| fr
    agent -->|"TCP 1 本 (gRPC)"| server
```

- execution trace を、runtime の FlightRecorder から直接取る (pprof の公開は要らない)
- ログを、アプリのロガーからプロセス内で受け取る。アプリが出力したバイト列をそのまま受け取る

##### ログの取り方

agent には、アプリごとに「ログをどこからどう読むか」を設定する。1 つの設定に書くのは次のとおり。

- 読み方 (下の表の種類)
- 読む場所 (ファイルのパス・待ち受けるポートなど)
- service (アプリの名前)
- そのアプリの pprof (このログと trace が同じアプリのものだと結びつけるため)

ログの形式は設定しない。agent が行の形から読み分ける (slog の JSON / テキスト・zap・zerolog・logrus・標準の log)。

| 種類 | 読むもの |
| --- | --- |
| file | ファイルを tail する。ローテーションに追従し、ワイルドカードでも指定できる。Kubernetes のログファイルの行頭 (時刻・stream・P/F) は剥がす |
| tcp / udp | 待ち受けたポートに届いた行。Fluent Bit などのログルーターから送ってもらう |
| journald | systemd journal |
| 標準入力 | パイプで渡された行 |
| ロガー | アプリのロガーから、同じプロセスの中で渡された行 (ライブラリ型だけ) |

| 置き方 | 環境 | 種類 | 読む場所 |
| --- | --- | --- | --- |
| daemon 型 | Kubernetes | file | ノードの `/var/log/pods` (コンテナの stdout / stderr) |
| daemon 型 | VM、ECS on EC2 | file / journald | アプリのログファイル / systemd journal |
| sidecar 型 | Kubernetes、Cloud Run、ECS Fargate | file | アプリと共有するボリューム上のファイル。アプリがそこにログを書く |
| sidecar 型 | ECS Fargate | tcp | FireLens (Fluent Bit) が転送するアプリの stdout / stderr |
| ライブラリ型 | どこでも | ロガー | アプリのロガー (ロガーアダプタ経由) |

- sidecar 型では、隣のコンテナの stdout を直接は読めない。そのため共有ボリュームのファイルか、ログルーターを経由する
- ECS Fargate では、FireLens の送り先を agent の tcp にする
- 参考
  - [Datadog Agent のログ収集](https://docs.datadoghq.com/agent/logs/)
  - [Datadog の Kubernetes でのログ収集](https://docs.datadoghq.com/containers/kubernetes/log/)
  - [Datadog の Cloud Run サイドカーでのログ収集](https://docs.datadoghq.com/serverless/google_cloud_run/containers/sidecar/nodejs/)
  - [Datadog の ECS Fargate での FireLens によるログ収集](https://datadoghq.com/blog/collect-fargate-logs-with-firelens)

### データフロー

```mermaid
sequenceDiagram
    participant T as アプリ
    participant A as gorviz-agent
    participant S as gorviz-server
    participant C as クライアント (GUI / CLI / MCP)

    A->>S: 接続を張る (agent から外向き)
    loop 常時
        A->>T: trace・goroutine dump を取得、ログを取り込む
        A->>A: 整形する
        A->>S: ログ・goroutine dump・trace の区切りを送る
    end
    loop 数秒ごと
        S->>S: 区切りの直近をリングバッファに持ち、Issue を検出する
    end
    opt Issue を検出したとき
        S->>S: その前後の区切りをディスクに保存する
    end
    C->>S: 画面 / API を開く
    alt リングバッファにある
        S->>S: リングバッファの区切りを読む
    else 消えている
        S->>S: 保存した区切りを読む
    end
    S->>S: 診断する
    S-->>C: 診断と根拠
```

#### agent と server の通信

- agent から server へ TCP を 1 本張り、その上で gRPC を流す。張った直後に agent が名乗る
- 観測データはまとめて gzip で圧縮して送る
- 切れたら agent が張り直し、つながらない間の分を上限まで持って送り直す。送り直しで同じ分が 2 回届いても、server は 1 回分だけ持つ
- keepalive で、止まった相手や黙って切られた接続を見つける

## コンポーネント仕様

各コンポーネントについて、責務と依存コンポーネントを書く。

### gorviz-agent

観測するアプリと同じホスト (または同じタスク) で動き、そのアプリから execution trace・goroutine dump・ログを取り、整形して server へ送る。

#### 責務

- アプリの pprof[^pprof] から execution trace・goroutine dump・goroutineleak プロファイル[^goroutineleak]を取る
- ログを取り込む。取り方は「[ログの取り方](#ログの取り方)」のとおり
- Kubernetes では、Pod の annotation から観測する Pod を見つける
- trace・goroutine dump を整形し、ログと一緒に server へ送る。生の trace は残さない
- server とつながらない間は、送るまでの区切りを上限まで持つ
- つないだときに、インスタンスの名前とアプリ (service) の名前を名乗る
- 判定・診断・画面は持たない。取ったものは送るまでしか持たない

#### 依存コンポーネント

- アプリの pprof
- ログの置き場所 (ファイル・journald・ログルーター)
- Kubernetes API (Pod の annotation を読むとき)
- gorviz-server

#### 取り方

##### execution trace

- 数秒ごとに区切って取り続ける
- 内訳や主因は全イベントから出すので、整形するときにイベントを捨てない

##### goroutine dump

- 間隔を空けて取る。STW[^stw] を伴うため

##### goroutineleak プロファイル

- goroutine dump の何回かに 1 回取る。GC を伴うため

#### Kubernetes での Pod の見つけ方

- annotation には、pprof のポートとログを取るコンテナを書く
- annotation のない Pod は観測しない

#### 制約

- 1 つのアプリを観測する agent は 1 つ (trace はプロセスで同時に 1 本しか取れないため)
- アプリにかける負荷は、本番に常時置ける程度 (Datadog の Go プロファイラと同程度) に収める

#### 観測データ

agent が送るものをまとめて観測データ[^observation]と呼ぶ。

| 運ぶもの | いつ | 単位 |
| --- | --- | --- |
| ログ | 数秒ごと | 前回送った後に増えた行をまとめて |
| goroutine dump | 取るたび | 1 回分 |
| execution trace | 区切り (数秒) ごと | 1 区切り。生の trace は送らない |

- trace を整形した形で送るのは、生の trace より小さく、server もこの形だけを持って読めば済むため。`go tool trace` に渡す生の trace は残さない

##### ログ (1 行)

| 項目 | 中身 |
| --- | --- |
| 番号 | 取り込んだ順の通し番号 |
| 時刻 | ログに書かれた時刻。なければ取り込んだ時刻 (どちらかを区別して持つ) |
| レベル・メッセージ | ログから読み取ったもの |
| 元の行 | 取り込んだ行そのもの |
| 取り込み元 | ログを取った口 (ファイル・TCP / UDP・journald・標準入力・ロガー) |
| trace との対応 | ロガーアダプタが振った番号。trace のログの印と同じ番号 |
| 所要時間 | ログに書かれていれば |

##### goroutine dump (1 回分)

| 項目 | 中身 |
| --- | --- |
| 時刻 | 取った時刻 |
| リーク判定 | runtime によるリーク判定をしたか (した / 今回はしない / 使えない / 切ってある) |
| goroutine の並び | その時点で生きている全 goroutine。1 つにつき下の表の項目を持つ |

| goroutine の項目 | 中身 |
| --- | --- |
| ID | goroutine の ID |
| 状態 | 状態 (待機理由を含む) と、その状態にいる時間 |
| 入口 | go 文で起動された関数 |
| 今いる関数 | goroutine dump の時点で止まっている関数 |
| 生成元 | go 文を書いた関数と場所 |
| リーク | runtime がリークと判定したか |

##### execution trace (1 区切り)

| 項目 | 中身 |
| --- | --- |
| 始まり | 区切りの始まりの壁時計の時刻。区切りの中の時刻は、ここからの経過時間で持つ |
| 長さ | 区切りの長さ。前の区切りと重なる部分は取り除いてある |
| 状態の変化 | G と P の状態の変化の列 (時刻順。全イベントを持ち、間引かない)。項目は下の表 |
| P の区間 | P ごとに、どの G がどの M の上で走ったかの区間 |
| 入口 | G ごとの入口の関数 |
| GC の区間 | stop-the-world (理由つき)・mark assist (どの G か)・concurrent mark |
| ヒープ | ヒープの大きさと GC の目標値の推移。GC が終わるたびの live heap と目標値 |
| 件数 | 区切りの中で作られた・終わった G の数と、始まった GC の回数 |
| ログの印 | ロガーアダプタが振った番号ごとに、書いた G とその時刻 |
| 集計 | 全イベントから数えた値。項目は下の表 |

| 状態の変化の項目 | G の変化 | P の変化 |
| --- | --- | --- |
| 時刻 | 変化した時刻 (区切りの始まりからの経過時間) | 変化した時刻 (区切りの始まりからの経過時間) |
| 対象 | どの G か | どの P か |
| 移った先 | 走っている / ローカルの実行待ちキュー / グローバルの実行待ちキュー / 待機 / システムコール / 終了 | 空いた / G を走らせ始めた |
| P・M | 使っている P と M | どの M の上か |
| 待機理由 | 待機に移ったときの理由。cgo の中にいるかも持つ | - |
| 止まった場所 | 止まった関数と、それを呼んだ利用者のコードの場所 | - |
| 作られたか | G が作られたときの変化か | - |
| 起こした G | 起こした G と、起こしたときにその G がいた関数と場所 | - |

| 集計の項目 | 中身 |
| --- | --- |
| P の数 | 区切りの中に現れた P の数 |
| 実行待ち | G が実行待ちだった時間の合計 |
| 待ち場所 | 種類・待機理由・入口・止まった関数・場所ごとに、回数・合計時間・最大 |
| 走り出すまで | 実行待ちに入ってから走り出すまでの時間の分布 |
| stop-the-world | 1 回の長さの分布 |
| GC の占有 | 短い時間幅ごとに見た、GC が P を占めた割合の最大 |
| P の使い方 | 全 P のうち G を走らせていた時間と、GC に使われた時間 |

### gorviz-server

agent から届いたデータを持ち、問題の検知と goroutine ごとの診断を行う。結果を GUI (Web) と HTTP API で提供する。

#### 責務

- agent からの接続を受け、agent ごとにセッションを持つ
- 届いた trace の区切りの直近を、agent ごとのリングバッファ[^ringbuffer]に持つ。上限はバイト数なので、アプリが忙しいほど持てる時間は短い
- 数秒ごとに、届いたログ・goroutine dump・区切りから Issue を検出する
- Issue をきっかけに、その前後の区切りをディスクに保存する。Issue と推移もディスクに持つ
- goroutine ごとの状態別の時間内訳[^breakdown]と主因[^cause]を出す (下の「[診断](#診断)」)
- GUI (Web) と HTTP API を出す
- 画面の再生に送るイベントには上限を設ける (ブラウザが毎フレーム適用するため)
- アプリには直接つながない

#### 依存コンポーネント

- ローカルディスク
- agent は server へ接続してくる側なので、依存先には入らない

#### データの保持

- 生の trace はどこにも持たない。agent が整形した区切りだけを扱う (生の trace は圧縮しても 1000 rps・31 日で約 600 GB になり、持ちきれない)
##### agent

- 送るまでの分だけ持つ

##### server のメモリ

agent ごとに持つ。

- 区切りの直近 (上限はバイト数。アプリが忙しいほど持てる時間は短い)
- ログの直近
- リーク判定用の goroutine dump の履歴

##### server のディスク

- 保存先はローカルディスクだけ (セルフホストのため)。利用者が決めるのは置き場所と、使ってよい容量だけ
- サービス単位で持つ。インスタンスは各行の項目として持つので、再起動やスケールで履歴が切れない
- 古くなるほど細かさを落として持つ。小さいものは必ず 31 日持ち、細かいものは残りの容量で持って、あふれたら古い方から捨てる

| データ | 期間 | 容量を超えたら |
| --- | --- | --- |
| Issue | 31 日 | 捨てない |
| 推移 (5 分刻み) | 31 日 | 捨てない |
| ログ (書いた goroutine の主因と止まっていた場所を添える) | 31 日 | 古い日から捨てる |
| 遅かった goroutine (1 分ごとの上位) | 31 日 | 捨てない |
| Issue の前後の区切り | 31 日 | 古い方から捨てる |
| 推移 (10 秒刻み) | 26 時間 | 古い方から捨てる |
| goroutine ごとの内訳 (入口・主因ごとの時間・止まっていた場所) | 7 日 | 古い方から捨てる |
| goroutine の状態の移り変わり | 1 日 | 古い方から捨てる |

- ログに主因と止まっていた場所を添えるので、区切りや内訳が消えたあとも、1 か月前のログから主因まで辿れる
- 画面には、その時刻に何が残っているか (再生できる / 内訳まで / 推移のみ) を示す

#### GUI

| 画面 | 役割 |
| --- | --- |
| Overview | 全体の様子 (未解決 Issue の数、goroutine 数・エラー・CPU 待ち・ヒープの推移) |
| Issues | 検出した問題の一覧 (31 日残る)。1 つ選ぶと、回数の推移と、見つかった回ごとに調べ始める先を示す |
| Logs | ログ一覧と件数の推移。行を開くと JSON のキーと値を見られ、`Goroutine →` で書いた goroutine (分からなければその時刻の Scheduler) を開く |
| Scheduler | 録画を GMP[^gmp] の概念図で再生する。最新を見ているあいだは新しい窓を追い、窓の端まで再生すると録画の続きをつなぐ。G を選ぶとその Goroutine を開く |
| Goroutine | 1 つの goroutine の診断と根拠 (その窓で書いたログを含む)。どの画面から入っても最後はここに行き着く |

- 見る対象 (agent) は画面ではなく、見出しの切り替えで選ぶ
- agent は service の名前 (launcher は起動するコマンドから取る) を名乗り、切り替えでは service ごとにインスタンスを並べる
- 選ぶのはインスタンスで、同じ service の複数のインスタンスを合わせて見ることはしない (区切りはインスタンスごとに別々のものなので)

#### 診断

診断には 3 種類のラベルを付ける。

| ラベル | 付ける先 | 値 |
| --- | --- | --- |
| Issue の種類 | Issue | `LEAK` / `CPU_WAIT_SPIKE` / `MEMORY_PRESSURE` |
| 確かさ[^certainty] | すべての診断 | `FACT` / `INFERENCE` |
| 主因[^cause] | goroutine の診断 | `RUNNING` / `CPU_WAIT` / `BLOCKED` / `IO_WAIT` / `CGO` / `SLEEP` / `GC` |

##### Issue の種類

| 種類 | 意味 | `FACT` | `INFERENCE` |
| --- | --- | --- | --- |
| `LEAK` | goroutine のリーク | runtime の goroutineleak プロファイルが判定 | 生成元ごとの待機中の数が減らずに増え続けている (古い Go 向け) |
| `CPU_WAIT_SPIKE` | P に乗れず待つ時間が多い | 固定の閾値を超えた | 普段との比較 |
| `MEMORY_PRESSURE` | GC が頻発 | GC の頻度が固定の閾値を超えた | - |

- Issue は runtime 層の問題だけにする
- 遅いリクエストやエラーの増加 (アプリ層) に気づくのは APM (Datadog など) の役目。gorviz はそこで見つけたログ・時間帯から goroutine と runtime を調べる入口を用意する (Logs の検索と時間の絞り込み)

##### 主因

状態別の時間内訳のうち最も長いもの。

| 主因 | 意味 |
| --- | --- |
| `RUNNING` | 自分で CPU を使っていた |
| `CPU_WAIT` | 動けるのに P が空かず待っていた |
| `BLOCKED` | ロック・channel で止まっていた |
| `IO_WAIT` | ネットワーク・ディスク・システムコールを待っていた |
| `CGO` | cgo で C のコードを実行していた (スタックに cgo の呼び出しがあるかで見分ける) |
| `SLEEP` | スリープ・タイマーで止まっていた |
| `GC` | GC に止められた / mark assist[^markassist] をさせられていた |

##### 確かさの付け方

Issue の確かさは「Issue の種類」の表のとおり。

| 診断 | `FACT` | `INFERENCE` |
| --- | --- | --- |
| 主因 | 前後の trace から決めた | goroutine dump だけから決めた |
| ログと goroutine の結びつき | ロガーアダプタが振った番号で結んだ | その時刻に走っていた goroutine が 1 つだけ |

##### 限界

- ハンドラが別の goroutine を起動して待っている場合、ログを書いた goroutine の主因は `BLOCKED` になる。子の goroutine までは追わない
- local run queue / global run queue の中身は runtime が出さないので、GMP の概念図では trace から推測して描く

## クライアントツール仕様

どれも gorviz-server の HTTP API のクライアントで、agent やアプリには直接つながない。

### gorviz-cli

人間が端末から使うコマンド。

#### 責務

- Issue の一覧を表示する

#### 依存コンポーネント

- gorviz-server の HTTP API

### gorviz-mcp

LLM エージェント向けの MCP[^mcp] サーバ。LLM エージェントとは stdio でやり取りする。

#### 責務

- 画面と同じ診断を JSON で返す
- 事実と推測を確かさ[^certainty]のラベルで分けて返す (LLM が推測を事実として扱わないように)
- 読み取りだけを行う

#### 依存コンポーネント

- gorviz-server の HTTP API

## gorviz package

アプリが import するライブラリ。agent の埋め込みとロガーアダプタからなり、どれを使うかは任意。

### agent の埋め込み

ライブラリ型の agent。

#### 責務

- agent をアプリと同じプロセスの中で動かす
- execution trace を runtime の FlightRecorder[^flightrecorder] から取る。pprof の公開は要らない
- ロガーアダプタからログを受け取る
- server へのつなぎ方と送るものは gorviz-agent と同じ

#### 依存コンポーネント

- gorviz-server

### ロガーアダプタ

アプリが使っているロガーを包み、ログを書くたびに gorviz の処理を挟む。

| ロガー | 包むもの |
| --- | --- |
| slog | ハンドラ |
| zap | Core と WriteSyncer |
| zerolog | Hook と Writer |
| logrus | Formatter |

#### 責務

- ログを書く goroutine の上で番号を振り、同じ番号を trace のログイベントとログ行の両方に書く。これでログ 1 行と、それを書いた goroutine を確実に結べる
- agent を埋め込んでいれば、出力したログ行をそのまま agent に渡す (slog だけは出力のバイト列を取り出せないので、JSON で書き直して渡す)
- ログの書式は変えない。番号のフィールドが 1 つ足されるだけ
- agent を別プロセスにする場合でも使える。使わなければ、ログの時刻から goroutine を推測する

#### 依存コンポーネント

- なし (agent を埋め込んでいれば、その agent にログを渡す)

## gorviz-launcher

ローカルで server と agent を 1 つのプロセスでまとめて動かす。

### 責務

- server と agent を 1 つのプロセスで起動する。プロセスは 1 つでも別々のプログラムのままで、間は gorviz-server と gorviz-agent と同じく TCP でつなぐ
- launcher 自身は組み立てるだけで、通信の相手にはならない
- アプリを起動するコマンドを渡すと、アプリも子プロセスとして起動する。アプリの標準出力・標準エラー出力をそのまま端末に出しながら、ログとして取り込む
- アプリが終わると launcher も終わる
- コマンドを渡さなければアプリは起動しない。すでに動いているアプリのログは標準入力へ流す

### 依存コンポーネント

- gorviz-server
- gorviz-agent

## ユーザーストーリー

| # | 誰が | いつ | 何をする | gorviz が返すもの |
| --- | --- | --- | --- | --- |
| 1 | 運用する開発者 | goroutine 数が増え続けているのに気づいた | Issues の `LEAK` から goroutine を開く | 生成元の go 文と止まっている場所 (file:line)・待機理由・件数の推移 |
| 2 | 運用する開発者 | 深夜のエラーに翌朝気づいた | Logs でそのエラーログの `Goroutine →` を押す | 保存済みの前後の区切りから、書いた goroutine の主因と止まっていた場所 |
| 3 | 運用する開発者 | 遅いリクエストを調べたい | 所要時間のあるログを選ぶ | そのリクエストの区間 (書いた時刻 − 所要時間 〜 書いた時刻) の内訳と主因 |
| 4 | 開発者 + LLM | 原因調査を LLM に任せたい | LLM が MCP でログを探し、診断を取り、コードを読む | 確かさ付きの診断を JSON で。LLM が原因と修正案を出す |
| 5 | 開発者 | 診断が本当か確かめたい / スケジューラを理解したい | Goroutine や Scheduler で再生する | GMP の概念図での再生 |

[^observation]: **観測データ**: agent がアプリから取って整形したもの。ログ・goroutine dump・execution trace の区切りの 3 つ
[^leak]: **goroutine のリーク**: 終わるはずの goroutine が何かを待ったまま止まり、溜まり続けること
[^breakdown]: **状態別の時間内訳**: ある区間で、その goroutine がどの状態 (実行中・CPU 待ち・ロック待ちなど) にどれだけいたか
[^cause]: **主因**: 状態別の時間内訳のうち最も長いもの
[^certainty]: **確かさ**: `FACT` (runtime の判定や trace / goroutine dump の記録から直接言える) と `INFERENCE` (gorviz が推移や比較から推し量った)
[^trace]: **execution trace**: goroutine ごとに「いつ走り、いつ待ち、いつ syscall に入ったか」をナノ秒単位で記録したもの。`go tool trace` で開く
[^dump]: **goroutine dump**: その瞬間の全 goroutine のスタックを並べたテキスト
[^goroutineleak]: **goroutineleak プロファイル**: Go 1.26 で入った、runtime 自身によるリーク判定。誰も起こせなくなった goroutine を GC の仕組みで見つけるので誤検知がない
[^pprof]: **pprof**: Go 標準のプロファイリングの仕組み。HTTP 経由で trace や goroutine dump を取れる
[^flightrecorder]: **FlightRecorder**: Go 1.25 で入った、trace の直近を常にメモリに持ち、必要なときに切り出せる機能
[^ringbuffer]: **リングバッファ**: 決まった大きさの入れ物に入れ続け、満杯になったら古いものから上書きする仕組み
[^gmp]: **GMP**: Go のスケジューラの仕組み。G (goroutine) を、P (実行の権利。GOMAXPROCS 個) を持った M (OS のスレッド) が走らせる
[^stw]: **STW (Stop The World)**: 全 goroutine を一瞬止めること。GC の一部や goroutine dump で起きる
[^markassist]: **mark assist**: 割り当ての多い goroutine が GC の作業を肩代わりさせられること。その間は本来の処理が進まない
[^mcp]: **MCP (Model Context Protocol)**: LLM エージェントが外部のツールやデータを呼び出すための共通の仕組み
