# gorviz 設計

用語の説明は脚注にまとめた。本文の番号 (`[1]` など) から飛べる。

## 目次

- [1. 概要](#1-概要)
- [2. モチベーション](#2-モチベーション)
- [3. アーキテクチャ設計](#3-アーキテクチャ設計)
  - [3.1 コンポーネント](#31-コンポーネント)
  - [3.2 依存関係](#32-依存関係)
  - [3.3 データフロー](#33-データフロー)
  - [3.4 サービスとインスタンス](#34-サービスとインスタンス)
- [4. コンポーネント仕様](#4-コンポーネント仕様)
  - [4.1 gorviz-agent](#41-gorviz-agent)
  - [4.2 gorviz-server](#42-gorviz-server)
- [5. クライアントツール仕様](#5-クライアントツール仕様)
  - [5.1 gorviz-cli](#51-gorviz-cli)
  - [5.2 gorviz-mcp](#52-gorviz-mcp)
- [6. gorviz package](#6-gorviz-package)
- [7. gorviz-launcher](#7-gorviz-launcher)
- [8. ユーザーストーリー](#8-ユーザーストーリー)

## 1. 概要

- Go のサーバアプリを作り運用する開発者が、パフォーマンスやバグの原因を Go ランタイムまで降りて調べるためのツール
- 問題の検知・ログ・時間帯を入口として、goroutine の粒度での診断を提供する。当時の Go ランタイムのスケジューラの動きを根拠として見せるため、開発者は事象を視覚的に理解できる

## 2. モチベーション

### 2.1 課題

- バグやパフォーマンス低下の原因は、アプリのコードやログを見ても分からず、Go ランタイムまで掘り下げないと辿り着けないことがある (goroutine が CPU・ロック・I/O・GC のどれを待っていたか、など)。ところが Go で開発する人でも、ランタイムのスケジューラまで理解している人はそう多くない。そのため調査が難航する。

- ランタイムの動きは execution trace[^trace] や goroutine dump[^dump] で見られるが、それらを使って効果的に調査するのは難しく、うまく扱えないことが多い。また、それらはその場で取るものであり、定常的に集めておく仕組みがない。そのため、後日に調べようとしても、問題が起きた瞬間のデータが残っていない。

### 2.2 アプローチ

- ランタイムを理解していなくても読める形で見せる
  - trace の生データをそのまま見せるのではなく、goroutine ごとの診断と、Go ランタイムのスケジューラの動きのアニメーションとして見せる
- 問題が起きた瞬間のランタイムの動きを、後から辿れる形で残しておく
  - 定常的に取り続け、問題が検知された前後の時間帯のものを一定期間残しておく。後日でもその瞬間を調べられるようにする
  - 問題の検知・ログ・時間帯を入口に、該当する goroutine に辿れるようにする

### 2.3 既存ツールとの差異

| ツール | できること | 残る課題 |
| --- | --- | --- |
| [pprof](https://pkg.go.dev/net/http/pprof) | goroutine の総数と goroutine dump を取る | - その場で取るだけで、定常的には残らない<br>- どの go 文の goroutine がリーク[^leak]しているかは、goroutine dump を手で読み解かないと分からない |
| [LeakProf](https://www.uber.com/en-US/blog/leakprof-featherlight-in-production-goroutine-leak-detection/) / [panicparse](https://github.com/maruel/panicparse) | goroutine dump を集計する | 止まっている場所の集計まで。ランタイムの中で何を待っていたかを読み解くのは利用者に残る |
| [`go tool trace`](https://pkg.go.dev/cmd/trace) / [gotraceui](https://gotraceui.dev/) | execution trace を開いて、スケジューリングまで見分けられる | - 生データを読み解くにはランタイムの知識が要る<br>- 定常的に取る仕組みがなく、問題が起きた瞬間が残らない<br>- 問題の検知・ログ・時間帯から、該当する goroutine に辿れない |

## 3. アーキテクチャ設計

### 3.1 コンポーネント

- gorviz-agent
  - 観測するアプリと同じホスト (または同じタスク) で動き、そのアプリから execution trace・ログを取り、整形して server へ送る
- gorviz-server
  - agent から届いたデータを持ち、問題の検知と goroutine ごとの診断を行う。結果を GUI (Web) と HTTP API で提供する
- gorviz-cli
  - 人間が端末から server の API を使う
- gorviz-mcp
  - LLM エージェントが MCP で server の API を使う

### 3.2 依存関係

矢印は「接続を張る / 呼び出す側 → 相手」。

#### 3.2.1 全体図

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

    agent -->|"execution trace・ログを取る"| app
    agent -->|"TCP 1 本 (gRPC)"| server
    server -->|"データを保存"| disk
    browser -->|"HTTP"| server
    cli -->|"HTTP API"| server
    mcp -->|"HTTP API"| server
    llm -->|"stdio"| mcp
```

#### 3.2.2 agent とアプリ

agent をアプリに対してどう配置するかを 3 通り用意する。

| | agent | 観測するアプリ | 具体環境例 |
| --- | --- | --- | --- |
| daemon 型 | gorviz-agent | 同じホストの複数のアプリ | Kubernetes |
| sidecar 型 | gorviz-agent | 同じタスク内のアプリ 1 つ | Fargate、Cloud Run |
| ライブラリ型 | gorviz package (agent の埋め込み) | そのアプリ自身 | agent を置けない環境 |

##### 3.2.2.1 daemon 型

```mermaid
flowchart LR
    subgraph node["ノード"]
        subgraph podA["Pod A"]
            appA["アプリ (pprof を公開)"]
        end
        subgraph podB["Pod B"]
            appB["アプリ (pprof を公開)"]
        end
        logs[("/var/log/pods")]
        agent["gorviz-agent (daemon)"]
    end
    k8s["Kubernetes API"]
    server["gorviz-server"]

    agent -->|"観測する Pod を見張る"| k8s
    agent -->|"HTTP で trace・goroutineleak プロファイルを取得"| appA
    agent -->|"HTTP で trace・goroutineleak プロファイルを取得"| appB
    appA -->|"stdout / stderr"| logs
    appB -->|"stdout / stderr"| logs
    agent -->|"ログファイルを読む"| logs
    agent -->|"アプリごとに TCP 1 本 (gRPC)"| server
```

- 1 つの agent が、同じノードに載った複数のアプリ (annotation で印を付けた Pod) をまとめて観測する。アプリごとに別のインスタンスとして server へ送る
- execution trace・goroutineleak プロファイルを、各アプリが公開する pprof の HTTP から取る
- ログを、ノードの `/var/log/pods` にある各コンテナのログファイルから取る

##### 3.2.2.2 sidecar 型

```mermaid
flowchart LR
    subgraph pod["Pod / タスク"]
        app["アプリのコンテナ (pprof を公開)"]
        vol[("共有ボリューム")]
        agent["gorviz-agent (sidecar container)"]
    end
    server["gorviz-server"]

    agent -->|"localhost の HTTP で trace・goroutineleak プロファイルを取得"| app
    app -->|"ログを書く"| vol
    agent -->|"ログファイルを読む"| vol
    agent -->|"TCP 1 本 (gRPC)"| server
```

- agent をアプリ 1 つの隣に置き、そのアプリだけを観測する
- execution trace・goroutineleak プロファイルを、アプリが公開する pprof の HTTP から localhost で取る
- ログを、共有ボリュームのファイルから取る。ECS Fargate では FireLens から tcp で受けることもできる

##### 3.2.2.3 ライブラリ型

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

##### 3.2.2.4 ログの取り方

agent には、アプリごとに「ログをどこからどう読むか」を設定する。1 つの設定に書くのは次のとおり。

- 読み方 (下の表の種類)
- 読む場所 (ファイルのパス・待ち受けるポートなど)
- サービスの名前
- そのアプリの pprof (このログと trace が同じアプリのものだと結びつけるため)

ログの形式は設定しない。agent が行の形から読み分ける (slog の JSON / テキスト・zap・zerolog・logrus・標準の log)。

| 種類 | 読むもの |
| --- | --- |
| file | ファイルを tail する。ローテーションに追従し、ワイルドカードでも指定できる。Kubernetes のログファイルの行頭 (時刻・stream・P/F) は剥がす |
| tcp / udp | 待ち受けたポートに届いた行。Fluent Bit などのログルーターから送ってもらう |
| 標準入力 | パイプで渡された行 |
| ロガー | アプリのロガーから、同じプロセスの中で渡された行 (ライブラリ型だけ) |

| 置き方 | 環境 | 種類 | 読む場所 |
| --- | --- | --- | --- |
| daemon 型 | Kubernetes | file | ノードの `/var/log/pods` (コンテナの stdout / stderr) |
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

### 3.3 データフロー

```mermaid
sequenceDiagram
    participant T as アプリ
    participant A as gorviz-agent
    participant S as gorviz-server
    participant C as クライアント (GUI / CLI / MCP)

    A->>S: 接続を張る (agent から外向き)
    loop 常時
        A->>T: trace・goroutineleak プロファイルを取得、ログを取り込む
        A->>A: 整形する
        A->>S: ログ・trace のチャンク・goroutine のスナップショットを送る
        S->>S: 届いたものをディスクに書く
    end
    loop 数秒ごと
        S->>S: ディスクの直近の観測データから Issue を検出する
    end
    C->>S: 画面 / API を開く
    S->>S: ディスクの観測データから goroutine を診断する
    S-->>C: 診断と根拠
```

#### 3.3.1 agent と server の通信

- agent から server へ TCP を 1 本張り、TLS で暗号化した上で gRPC を流す。暗号化した直後に、agent がハンドシェイクでインスタンスの名前・サービスの名前・トークンを送る (「[通信の暗号化と認証](#426-通信の暗号化と認証)」)
- 観測データはまとめて gzip で圧縮して送る
- 切れたら agent が張り直し、つながらない間の分を上限まで持って送り直す。送り直しで同じ分が 2 回届いても、server は 1 回分だけ持つ
- keepalive で、止まった相手や黙って切られた接続を見つける

### 3.4 サービスとインスタンス

gorviz は、観測するアプリを「サービス」と「インスタンス」の 2 段で扱う。

| | 指すもの | 扱い |
| --- | --- | --- |
| サービス | 同じアプリのインスタンスをまとめたもの。デプロイやスケールでインスタンスが入れ替わっても、サービスは同じまま | Overview・Issues・Logs で見る単位。ログ・Issue・メトリクスなどを、インスタンスをまたいで 1 本の履歴として持つ |
| インスタンス | アプリのプロセス 1 つ。コンテナで動かす場合は、コンテナの 1 回分の起動にあたる | Scheduler・Goroutine で見る単位。execution trace・goroutine のスナップショットを持ち、goroutine を区別する |

- どちらの名前も、agent がハンドシェイクで server に送る

#### 3.4.1 インスタンスのライフサイクル

インスタンスは、アプリのプロセスが始まってから終わるまで。インスタンスに起きることと、gorviz での扱いは次のとおり。

| ライフイベント | 扱い |
| --- | --- |
| 始まった (デプロイ・スケールアウト) | 新しいインスタンスとして記録し、サービスの時間軸に加える |
| 終わった (デプロイ・スケールイン) | 終わりを記録する。データは保持期間まで残る |
| 同じ名前のまま再起動した (クラッシュによるコンテナの再起動など) | 再起動の前後を、名前が同じ別々のインスタンスとして扱う。前のインスタンスの終わりと、新しいインスタンスの始まりを記録する |

- インスタンスの始まりと終わりは、server がサービスごとに記録する
- goroutine は、インスタンスと goroutine の ID の組で区別する

## 4. コンポーネント仕様

各コンポーネントについて、責務と依存コンポーネントを書く。

### 4.1 gorviz-agent

観測するアプリと同じホスト (または同じタスク) で動き、そのアプリから execution trace・ログを取り、整形して server へ送る。

#### 4.1.1 責務

##### 4.1.1.1 観測するアプリ

| 置き方 | 観測するアプリの決め方 |
| --- | --- |
| daemon 型 | 自分のノードの Pod のうち、annotation で印を付けた Pod (「[Kubernetes での Pod の見つけ方](#415-kubernetes-での-pod-の見つけ方)」) |
| sidecar 型 | 利用者が指定したアプリ |
| ライブラリ型 | 組み込まれたアプリ自身 |

##### 4.1.1.2 取得するもの

| 取得するもの | 取得元 |
| --- | --- |
| execution trace | アプリの pprof[^pprof] (「[pprof から取るもの](#413-pprof-から取るもの)」) |
| goroutineleak プロファイル[^goroutineleak] | アプリの pprof |
| ログ | 「[ログの取り方](#3224-ログの取り方)」のとおり |

##### 4.1.1.3 server に送るもの

| 送るもの | 中身 |
| --- | --- |
| ハンドシェイク | インスタンスの名前とサービスの名前 (「[サービスとインスタンス](#34-サービスとインスタンス)」)。つないだときに送る |
| ログ | 取り込んだ行を読み取ったもの |
| execution trace のチャンク | 整形したもの。生の trace は送らない |
| goroutine のスナップショット | trace のチャンクの終わりの時点で生きている全 goroutine の状態。trace から作る (「[goroutine のスナップショットの作り方](#414-goroutine-のスナップショットの作り方)」) |
| リーク判定 | goroutineleak プロファイルを読み取ったもの |

- 各項目は「[観測データ](#417-観測データ)」のとおり
- 送り方 (つながらない間に持っておく分・送り直し) は「[agent と server の通信](#331-agent-と-server-の通信)」のとおり

#### 4.1.2 依存コンポーネント

- アプリの pprof
- ログの置き場所 (ファイル・ログルーター)
- Kubernetes API (Pod の annotation を読むとき)
- gorviz-server

#### 4.1.3 pprof から取るもの

##### 4.1.3.1 execution trace

- 数秒ごとのチャンク[^chunk]に切り分けて取り続ける
- チャンクごとに、G と P の状態の変化の列・P ごとの実行区間・GC の区間・ヒープの推移・ログの印・集計に整形する。項目は「[execution trace (1 チャンク)](#4174-execution-trace-1-チャンク)」のとおり

##### 4.1.3.2 goroutineleak プロファイル

- `LEAK` を誤検知なしで判定するために取る
- 間隔を空けて取る。取るたびに GC が 1 回走るため
- リークと判定された goroutine だけを、スタックごとの件数で取る。全 goroutine を書き出す形式は、書き出す間ずっと STW[^stw] になるので使わない

#### 4.1.4 goroutine のスナップショットの作り方

goroutine のスナップショットは、goroutine dump の代わりになるもの。goroutine dump はアプリ全体を止めるので取らず、execution trace から作る。trace は、チャンクの中でイベントを出さなかった goroutine についても、今の状態とスタックを記録する。

項目ごとの作り方は次のとおり。

| 項目 | 元にするデータ | 作り方 |
| --- | --- | --- |
| 状態・入口・今いる関数 | trace の、チャンクの終わりの状態の記録 (状態とスタック) | そのまま使う。入口はスタックの一番下、今いる関数は一番上 |
| 待機理由 | trace の、待機に入ったときのイベント (理由つき) | trace のチャンクの終わりの記録には入らないので、goroutine ごとに覚えて次の trace のチャンクに持ち越す |
| 待っている時間 | trace の、待機に入ったときのイベント (時刻つき) | 待機に入った時刻を覚え、trace のチャンクの終わりとの差にする |
| 生成元 | trace の、作られたときのイベント (作った側のスタックつき) | trace のチャンクの終わりの記録には入らないので、goroutine ごとに覚える |
| リーク | goroutineleak プロファイル | プロファイルのスタックと、goroutine のスタックを突き合わせる |

リークの突き合わせ方は次のとおり。

- goroutineleak プロファイルは「このスタックの goroutine が N 個リークしている」という件数で届く。どの goroutine かは書かれない
- スナップショットに同じスタックの goroutine がちょうど N 個あれば、その全部をリークとする
- N 個より多ければ、どれがリークかは決められない。リークした goroutine は二度と起きず待っている時間が伸び続けるので、同じスタックの M 個のうち待っている時間が長い順に N 個をリークと推定する
- どちらの場合も、「このスタックの M 個のうち N 個がリーク」という件数はスタック単位で持つ

##### 4.1.4.1 goroutine の識別

goroutine は「インスタンス + goroutine の ID」で識別する (「[サービスとインスタンス](#34-サービスとインスタンス)」)。agent は、同じ名前のまま中のプロセスが入れ替わったことを次のように見分ける。

| 置き方 | プロセスが入れ替わったことの見分け方 |
| --- | --- |
| ライブラリ型 | agent もアプリと一緒に起動し直すので、そのまま新しいインスタンスになる |
| daemon 型 | アプリのコンテナの ID が変わる。Kubernetes API で Pod を見ているので取れる |
| sidecar 型 | 最大の goroutine の ID が減る。agent が再起動しても、減っていなければ同じインスタンスのまま |

goroutine ごとに覚えている情報 (待機理由・待ち始めた時刻・生成元) は、次のように扱う。

| 場面 | 例 | 覚えている情報 |
| --- | --- | --- |
| 新しいインスタンスになった | デプロイ・クラッシュによるコンテナの再起動 | 引き継がない。前のインスタンスの分は、そのまま server に残る |
| agent だけが再起動した | agent のコンテナの再起動・更新 | agent の記憶は消えるので、server が前のスナップショットから同じ goroutine の分を引き継ぐ |

#### 4.1.5 Kubernetes での Pod の見つけ方

daemon 型の agent は、自分のノードに載っている Pod のうち、観測してほしいと印を付けられた Pod だけを観測する。印は、利用者がアプリの Pod に付ける annotation[^annotation] で表す。

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
spec:
  template:
    metadata:
      annotations: # Deployment 自体ではなく Pod のテンプレートに書く
        gorviz/pprof-port: "6060" # agent はこの Pod の IP とこのポートで pprof に取りに行く
        gorviz/log-container: app # stdout / stderr をログとして取るコンテナ。envoy のログは取らない
    spec:
      containers:
        - name: app
          image: example/checkout
          ports:
            - containerPort: 6060
        - name: envoy
          image: envoyproxy/envoy
```

- ログは、ノードの `/var/log/pods` にある、指定したコンテナのログファイルから読む
- agent は Kubernetes API で自分のノードの Pod を見張り、annotation の付いた Pod が現れたら観測を始め、消えたらやめる
- annotation の付いていない Pod は、同じノードにあっても観測しない。観測する Pod を利用者が明示的に選ぶため

#### 4.1.6 制約

- 1 つのアプリを観測する agent は 1 つ (trace はプロセスで同時に 1 本しか取れないため)
- アプリにかける負荷は、本番に常時置ける程度 (Datadog の Go プロファイラと同程度) に収める
- agent が起動していない間に待機に入った・作られた goroutine は、スナップショットの情報が一部不十分になる
  - 原因
    - 待機理由・待ち始めた時刻・生成元は、その瞬間の trace のイベントにしか入らないため
  - 対処
    - 待機理由は、trace のチャンクの終わりの状態の記録にあるスタックの上の方の runtime の関数 (channel の受信・ロック・スリープ・ネットワークなど) から推定する (`INFERENCE`)
    - 待っている時間は、agent が見始めてからの下限で持つ
    - 生成元は、入口の関数で代用する

#### 4.1.7 観測データ

agent が送るものをまとめて観測データ[^observation]と呼ぶ。

| 運ぶもの | いつ | 単位 |
| --- | --- | --- |
| ログ | 数秒ごと | 前回送った後に増えた行をまとめて |
| goroutine のスナップショット | trace のチャンク (数秒) ごと | 1 回分 |
| リーク判定 | goroutineleak プロファイルを取るたび | 1 回分 |
| execution trace | チャンク (数秒) ごと | 1 チャンク。生の trace は送らない |

- trace を整形した形で送るのは、生の trace より小さく、server もこの形だけを持って読めば済むため。`go tool trace` に渡す生の trace は残さない

##### 4.1.7.1 ログ (1 行)

| 項目 | 中身 |
| --- | --- |
| 番号 | 取り込んだ順の通し番号 |
| 時刻 | ログに書かれた時刻。なければ取り込んだ時刻 (どちらかを区別して持つ) |
| レベル・メッセージ | ログから読み取ったもの |
| 元の行 | 取り込んだ行そのもの |
| 取り込み元 | ログを取った口 (ファイル・TCP / UDP・標準入力・ロガー) |
| trace との対応 | ロガーアダプタが振った番号。trace のログの印と同じ番号 |
| 所要時間 | ログに書かれていれば |

##### 4.1.7.2 goroutine のスナップショット (1 回分)

| 項目 | 中身 |
| --- | --- |
| 時刻 | trace のチャンクの終わりの時刻 |
| goroutine の並び | その時点で生きている全 goroutine。1 つにつき下の表の項目を持つ |

| goroutine の項目 | 中身 |
| --- | --- |
| ID | goroutine の ID |
| 状態 | 状態と待機理由。待機理由をスタックから推定したかも持つ |
| 待っている時間 | その状態にいる時間。agent の起動前から続いていて下限しか分からないかも持つ |
| 入口 | go 文で起動された関数 |
| 今いる関数 | trace のチャンクの終わりに止まっている関数 |
| 生成元 | go 文を書いた関数と場所 |
| リーク | runtime がリークと判定したか |

##### 4.1.7.3 リーク判定 (1 回分)

| 項目 | 中身 |
| --- | --- |
| 時刻 | 取った時刻 |
| 状態 | runtime によるリーク判定をしたか (した / 使えない / 切ってある) |
| リークした goroutine | スタックごとの件数 |

##### 4.1.7.4 execution trace (1 チャンク)

| 項目 | 中身 |
| --- | --- |
| 始まり | チャンクの始まりの壁時計の時刻。チャンクの中の時刻は、ここからの経過時間で持つ |
| 長さ | チャンクの長さ。前のチャンクと重なる部分は取り除いてある |
| 状態の変化 | G と P の状態の変化の列 (時刻順。全イベントを持ち、間引かない)。項目は下の表 |
| P の区間 | P ごとに、どの G がどの M の上で走ったかの区間 |
| 入口 | G ごとの入口の関数 |
| GC の区間 | stop-the-world (理由つき)・mark assist (どの G か)・concurrent mark |
| ヒープ | ヒープの大きさと GC の目標値の推移。GC が終わるたびの live heap と目標値 |
| 件数 | チャンクの中で作られた・終わった G の数と、始まった GC の回数 |
| ログの印 | ロガーアダプタが振った番号ごとに、書いた G とその時刻 |
| 集計 | 全イベントから数えた値。項目は下の表 |

| 状態の変化の項目 | G の変化 | P の変化 |
| --- | --- | --- |
| 時刻 | 変化した時刻 (チャンクの始まりからの経過時間) | 変化した時刻 (チャンクの始まりからの経過時間) |
| 対象 | どの G か | どの P か |
| 移った先 | 走っている / ローカルの実行待ちキュー / グローバルの実行待ちキュー / 待機 / システムコール / 終了 | 空いた / G を走らせ始めた |
| P・M | 使っている P と M | どの M の上か |
| 待機理由 | 待機に移ったときの理由。cgo の中にいるかも持つ | - |
| 止まった場所 | 止まった関数と、それを呼んだ利用者のコードの場所 | - |
| 作られたか | G が作られたときの変化か | - |
| 起こした G | 起こした G と、起こしたときにその G がいた関数と場所 | - |

| 集計の項目 | 中身 |
| --- | --- |
| P の数 | チャンクの中に現れた P の数 |
| 実行待ち | G が実行待ちだった時間の合計 |
| 待ち場所 | 種類・待機理由・入口・止まった関数・場所ごとに、回数・合計時間・最大 |
| 走り出すまで | 実行待ちに入ってから走り出すまでの時間の分布 |
| stop-the-world | 1 回の長さの分布 |
| GC の占有 | 短い時間幅ごとに見た、GC が P を占めた割合の最大 |
| P の使い方 | 全 P のうち G を走らせていた時間と、GC に使われた時間 |

### 4.2 gorviz-server

agent から届いたデータを持ち、問題の検知と goroutine ごとの診断を行う。結果を GUI (Web) と HTTP API で提供する。

#### 4.2.1 責務

##### 4.2.1.1 受け取るもの

| 受け取るもの | 送り手 |
| --- | --- |
| ハンドシェイク | agent。接続を張った直後に送ってくる。server はインスタンスごとに受け口を持つ |
| 観測データ (ログ・trace のチャンク・goroutine のスナップショット・リーク判定) | agent |

##### 4.2.1.2 行うこと

| 処理 | いつ | 中身 |
| --- | --- | --- |
| Issue の検出 | 数秒ごと | 届いた観測データから Issue を検出する |
| goroutine の診断 | クライアントが求めたとき | 観測データから、goroutine ごとの状態別の時間内訳[^breakdown]・主因[^cause]・止まっていた場所を出す (「[診断](#425-診断)」) |
| データの保持 | 常に | 届いた観測データをディスクに書き、古くなったら間引く。Issue の発生・エラーログの前後の execution trace は長く残す (「[データの持ち方](#423-データの持ち方)」) |

##### 4.2.1.3 提供するもの

| 提供するもの | 使う側 |
| --- | --- |
| GUI (Web) | ブラウザ (「[GUI](#424-gui)」) |
| HTTP API (JSON) | gorviz-cli・gorviz-mcp |

#### 4.2.2 依存コンポーネント

- ローカルディスク

#### 4.2.3 データの持ち方

- 生の trace はどこにも持たない。agent が整形した trace のチャンクだけを扱う
- データはサービスの下にインスタンスがぶら下がる形で持つ。インスタンスが入れ替わっても、サービスの履歴は 1 本につながる

##### 4.2.3.1 agent

- 送るまでの分だけ持つ
- 待機理由・待ち始めた時刻・生成元は、goroutine ごとに、インスタンスが続く間覚える

##### 4.2.3.2 server

- 届いた観測データはすべてディスクに書き、読むのもディスクから。保存先はローカルディスクだけ (セルフホストのため)。利用者が決めるのは置き場所と、データごとの期間だけ
- サービスとインスタンスの 2 層で管理する (「[サービスとインスタンス](#34-サービスとインスタンス)」)。インスタンスが入れ替わっても、サービスの履歴は切れない

###### 4.2.3.2.1 ファイル構成

```sh
<保存先>/
├── server/                                      # server 自身のもの
│   ├── tls/
│   │   ├── ca.pem                               # 自分で作った CA の証明書
│   │   └── server.pem                           # server の証明書
│   └── auth-tokens.jsonl                        # 発行したトークン
└── services/
    └── <サービス>/
        ├── service/                             # サービス単位 (Overview・Issues・Logs)
        │   ├── metrics/2026-11-21.jsonl         # メトリクス (goroutine 数・エラー数・CPU 待ち・ヒープなど、10 秒刻み)
        │   ├── issues/                          # Issue
        │   │   └── 2026-11-21/                  # 日ごと
        │   │       └── <種類>--<対象>--<ハッシュ>.json # その日の発生の一覧 (発生ごとに影響の大きかった goroutine を含む)。例: LEAK--handle-server.go-42-chan-send--3f9a1c.json
        │   ├── logs/2026-11-21T14.jsonl         # ログ
        │   └── instance-lifecycle/2026-11-21.jsonl # インスタンスの始まりと終わり
        └── instances/                           # インスタンス単位 (Scheduler・Goroutine)
            └── <インスタンスの名前>/             # 例: checkout-7d9f-abcde
                ├── execution-traces/            # 整形済みの execution trace
                │   ├── 2026-11-21T14.pb.gz
                │   └── 2026-11-21T14.index
                ├── execution-traces-around-issues/  # Issue の発生の前後の execution trace
                │   └── 2026-11-21/                  # 日ごと
                │       └── <種類>--<対象>--<ハッシュ>/   # Issue のファイル名と同じ
                │           └── <発生した時刻>.pb.gz     # 例: 2026-11-21T14-03-12.pb.gz
                ├── execution-traces-around-error-logs/  # エラーログの前後の execution trace
                │   └── 2026-11-21/                  # 日ごと
                │       └── <メッセージの形>--<ハッシュ>/   # 例: request-failed-user-id-n--8b21e0
                │           └── <書いた時刻>.pb.gz
                ├── goroutine-snapshots/         # goroutine のスナップショット
                │   └── 2026-11-21T14.pb.gz
                └── goroutine-state-breakdowns/  # goroutine ごとの状態別の時間内訳
                    └── 2026-11-21.jsonl
```

- `.jsonl` は 1 行 1 件のデータ。`.pb.gz` は agent から届いた protobuf を gzip したもので、届いた形のまま書く
- ファイルは時刻 (UTC) で区切り、ファイル名を区切りの始まりの時刻にする
  - 量の多いもの (execution trace・goroutine のスナップショット・ログ) は 1 時間ごと (`2026-11-21T14`)
  - それ以外は 1 日ごと (`2026-11-21`)
  - Issue と、Issue の発生・エラーログの前後の execution trace は、日ごとのディレクトリの下に置く。1 つの Issue は、発生した日ごとのファイルに分かれる
- インスタンスのディレクトリ名は、インスタンスの名前。同じ名前のまま新しいインスタンスになったときは、同じディレクトリに続けて書く。どこで切り替わったかは `service/instance-lifecycle/` で分かる
- 文字列から作る名前は、`<読める形>--<ハッシュ>` の形にそろえる
  - 読める形は、元の文字列のうちファイル名に使えない文字を `-` に置き換え、短く切り詰めたもの
  - ハッシュは、元の文字列から作る短い値。切り詰めても別のものと名前が重ならないようにする。同じ文字列からは必ず同じ名前になる

| 名前 | 元の文字列 | 形 | 例 |
| --- | --- | --- | --- |
| Issue のファイル | Issue の種類と対象 | `<種類>--<対象>--<ハッシュ>` | `LEAK--handle-server.go-42-chan-send--3f9a1c` |
| ログの前後の execution trace のディレクトリ | メッセージの形 (メッセージの値の部分をならしたもの) | `<メッセージの形>--<ハッシュ>` | `request-failed-user-id-n--8b21e0` |

- 同じ Issue は、どの日のファイルでも同じ名前になる。Issues で 1 件を開くときは、同じ名前のファイルを日をまたいで集める。同じ形のメッセージは同じディレクトリにまとまる
- Issue のファイルには、発生[^occurrence]ごとに次を持つ
  - 発生した時刻 (UTC) とインスタンス
  - 前後の execution trace のファイル名。`execution-traces-around-issues/<日>/<Issue のファイル名>/<発生した時刻>.pb.gz`
  - 影響の大きかった goroutine の上位。前後の execution trace が消えても、ここから調べ始められる
- Issue の発生の前後の execution trace は、発生したインスタンスの `execution-traces-around-issues/` へ写す。Issue のファイルの発生の一覧から、そのファイルを引ける
- エラーログ (`ERROR` 以上) の前後の execution trace も、書いたインスタンスの `execution-traces-around-error-logs/` へ写す。ログの行から、そのファイルを引ける
  - 毎回写す。同じディレクトリで、前後の範囲が前に写したものと重なるときは、重なった分は写し直さない
- execution trace は、チャンクを 1 つずつ gzip して時刻順につなげて書く。索引には、チャンクごとに始まりと終わりの時刻と、ファイルの中の位置を持つ。読むときは、索引で時刻から位置を引き、その範囲のチャンクだけを読む

###### 4.2.3.2.2 保持期間

データごとに、何に使うかから持つ期間を決める。期間は利用者が変えられる。表の期間は、変えなかったときの期間。

| データ | 置き場所 | 使い道 | 期間 |
| --- | --- | --- | --- |
| メトリクス (10 秒刻み) | `service/metrics/` | Overview の推移。1 か月の傾向を見る | 31 日 |
| Issue | `service/issues/` | Issues の一覧と、発生ごとの調べ始め | 31 日 |
| ログ (書いた goroutine を添える) | `service/logs/` | Logs と、書いた goroutine への動線 | 31 日 |
| インスタンスの始まりと終わり | `service/instance-lifecycle/` | 時間軸の印と、同じ名前のインスタンスの区切り | 31 日 |
| execution trace | `instances/<インスタンス>/execution-traces/` | 直近の、スケジューラの動きの再生 (Scheduler) と、ログ 1 行のリクエストの区間の goroutine の診断 | 7 日 |
| Issue の発生の前後の execution trace | `instances/<インスタンス>/execution-traces-around-issues/` | 後日でも、Issue の発生の前後のスケジューラの動きを再生し、goroutine を診断する | 31 日 |
| エラーログの前後の execution trace | `instances/<インスタンス>/execution-traces-around-error-logs/` | 後日でも、エラーログを書いた前後のスケジューラの動きを再生し、書いた goroutine を診断する | 31 日 |
| goroutine のスナップショット | `instances/<インスタンス>/goroutine-snapshots/` | execution trace が消えたあとの、goroutine の状態・生成元・リーク | 31 日 |
| goroutine ごとの状態別の時間内訳 (入口・主因ごとの時間・止まっていた場所) | `instances/<インスタンス>/goroutine-state-breakdowns/` | execution trace が消えたあとの、主因と止まっていた場所 | 31 日 |

- execution trace だけ 7 日にしているのは、ほかのデータより桁違いに大きいため (負荷に比例して増える)。7 日を過ぎると、スケジューラの動きの再生と、リクエストの区間に合わせた正確な診断ができなくなる (Issue の発生・エラーログの前後に写した分を除く)。goroutine の状態・主因・止まっていた場所は、スナップショットと状態別の時間内訳から引き続き分かる
- 期間を過ぎたものは、ファイルごと捨てる。空になったディレクトリ (インスタンスのディレクトリなど) も捨てる

#### 4.2.4 GUI

| 画面 | 単位 | 役割 |
| --- | --- | --- |
| Overview | サービス | 全体の様子 (未解決 Issue の数、goroutine 数・エラー・CPU 待ち・ヒープの推移) |
| Issues | サービス | 検出した問題の一覧 (31 日残る)。1 つ選ぶと、発生の回数の推移と、発生ごとに調べ始める先を示す |
| Logs | サービス | ログ一覧と件数の推移。行を開くと JSON のキーと値を見られ、`Goroutine →` で書いた goroutine (分からなければその時刻の Scheduler) を開く |
| Scheduler | インスタンス | 録画を GMP[^gmp] の概念図で再生する。最新を見ているあいだは新しい窓を追い、窓の端まで再生すると録画の続きをつなぐ。G を選ぶとその Goroutine を開く |
| Goroutine | インスタンス | 1 つの goroutine の診断と根拠 (その窓で書いたログを含む)。どの画面から入っても最後はここに行き着く |

- 見るサービスは、画面ではなく見出しの切り替えで選ぶ
- サービス単位の画面では、インスタンスをまたいで 1 本に並べ、各行にどのインスタンスのものかを出す
- Scheduler と Goroutine はインスタンス単位で見る。trace のチャンクはインスタンスごとに別のものなので、まとめない。サービス単位の画面の行から移るとき、その行のインスタンスに決まる
- サービスの名前とインスタンスの名前は、agent がハンドシェイクで送ったものを使う (launcher はサービスの名前を起動するコマンドから取る)
- `LEAK` の Issue は、スタックごとに「同じ場所で待っている M 個のうち N 個がリーク」と件数で示す
- Scheduler と Goroutine では、goroutine ごとのリークを、確かさ (`FACT` / `INFERENCE`) が分かる形で示す
- インスタンスの始まりと終わり・再起動は、時間軸に印として示す (「[サービスとインスタンス](#34-サービスとインスタンス)」)

#### 4.2.5 診断

goroutine の診断は、調べる期間に 1 つの goroutine が何に止まっていたかを判定するもの。調べる期間は入口で決まる (ログの所要時間から分かるリクエストの区間、Issue の前後、Scheduler で選んだ時刻の前後など)。

| 出すもの | 中身 | 元にするデータ |
| --- | --- | --- |
| 状態別の時間内訳 | その期間に、実行中・CPU 待ち・ロック待ちなどの各状態にどれだけいたか | trace のチャンクの状態の変化 |
| 主因 | 状態別の時間内訳のうち最も長い状態 | 状態別の時間内訳。trace のチャンクが残っていないときは goroutine のスナップショットの状態と待機理由 |
| 止まっていた場所 | 最も長く止まっていた関数と場所 (file:line) | trace のチャンクの状態の変化のスタック。trace のチャンクが残っていないときは goroutine のスナップショットの今いる関数 |
| リーク | runtime がリークと判定したか | goroutine のスナップショット |
| 根拠 | 状態別の時間内訳と、その期間の GMP[^gmp] の概念図での再生 | trace のチャンク |

Issue と goroutine の診断には、3 種類のラベルを付ける。

| ラベル | 付ける先 | 値 |
| --- | --- | --- |
| Issue の種類 | Issue | `LEAK` / `CPU_WAIT_SPIKE` / `MEMORY_PRESSURE` |
| 確かさ[^certainty] | Issue と goroutine の診断 | `FACT` / `INFERENCE` |
| 主因[^cause] | goroutine の診断 | `RUNNING` / `CPU_WAIT` / `BLOCKED` / `IO_WAIT` / `CGO` / `SLEEP` / `GC` |

##### 4.2.5.1 Issue の種類

| 種類 | 意味 | `FACT` | `INFERENCE` | 影響の大きかった goroutine |
| --- | --- | --- | --- | --- |
| `LEAK` | goroutine のリーク | runtime の goroutineleak プロファイルが判定 | 生成元ごとの待機中の数が減らずに増え続けている (古い Go 向け) | リークした goroutine (スタックごとの件数) |
| `CPU_WAIT_SPIKE` | P に乗れず待つ時間が多い | 固定の閾値を超えた | 普段との比較 | CPU 待ちの長かった goroutine |
| `MEMORY_PRESSURE` | GC が頻発 | GC の頻度が固定の閾値を超えた | - | mark assist をさせられた時間の長かった goroutine |

- 影響の大きかった goroutine は、発生ごとに上位を Issue のファイルに持つ (「[server](#4232-server)」)

- Issue は runtime 層の問題だけにする
- 遅いリクエストやエラーの増加 (アプリ層) に気づくのは APM (Datadog など) の役目。gorviz はそこで見つけたログ・時間帯から goroutine と runtime を調べる入口を用意する (Logs の検索と時間の絞り込み)

##### 4.2.5.2 主因

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

##### 4.2.5.3 確かさの付け方

Issue の確かさは「Issue の種類」の表のとおり。

| 判定するもの | `FACT` | `INFERENCE` |
| --- | --- | --- |
| 主因 | trace のチャンクの状態の変化から決めた | goroutine のスナップショットの、スタックから推定した待機理由だけで決めた |
| ログと goroutine の結びつき | ロガーアダプタが振った番号で結んだ | その時刻に走っていた goroutine が 1 つだけ |
| goroutine ごとのリーク | 同じスタックの goroutine がちょうどリークの件数だけあった | 同じスタックの goroutine のうち、待っている時間が長い順に件数分を選んだ |

##### 4.2.5.4 限界

- ハンドラが別の goroutine を起動して待っている場合、ログを書いた goroutine の主因は `BLOCKED` になる。子の goroutine までは追わない
- local run queue / global run queue の中身は runtime が出さないので、GMP の概念図では trace から推測して描く

#### 4.2.6 通信の暗号化と認証

server が待ち受ける口は、agent からの接続と、GUI・HTTP API の 2 つ。同じネットワークの中に置く場合でも、どちらも暗号化する。観測データには、ログの中身や関数名・ファイルの場所が入るため。

##### 4.2.6.1 agent と server の通信

- TCP を張ったら、まず TLS で暗号化する。ハンドシェイクと gRPC は、その上で流す
- agent は、CA で server の証明書を確かめる
- agent はハンドシェイクでトークンを送る。server は、知らないトークンの接続を断る
- トークンは server 全体で共通のもの。複数を発行でき、1 つずつ無効にできる (入れ替えや、漏れたときに止めるため)
- 求められれば、agent にもクライアント証明書を持たせる (相互 TLS)
- 平文でつなぐのは、利用者が明示したときと、gorviz-launcher の中 (同じプロセスの中の localhost) だけ

##### 4.2.6.2 GUI と HTTP API

| 使う側 | 認証 |
| --- | --- |
| ブラウザ (GUI) | ログイン |
| gorviz-cli・gorviz-mcp | トークン |

- agent との通信と同じ証明書で、TLS で暗号化する
- gorviz-cli・gorviz-mcp のトークンも、複数を発行でき、1 つずつ無効にできる
- 組織の SSO を使う場合は、認証を前段の認証付きリバースプロキシに任せることもできる

##### 4.2.6.3 証明書

| 場面 | 証明書 |
| --- | --- |
| 利用者が証明書を用意できる | 利用者が差し込む (社内の CA・cert-manager・Let's Encrypt など) |
| 用意できない | server が初回起動時に自分で CA と証明書を作り、CA のハッシュを示す。agent とクライアントは、そのハッシュで server を確かめる |

## 5. クライアントツール仕様

どれも gorviz-server の HTTP API のクライアントで、agent やアプリには直接つながない。server にはトークンで認証してつなぐ (「[通信の暗号化と認証](#426-通信の暗号化と認証)」)。

### 5.1 gorviz-cli

人間が端末から使うコマンド。

#### 5.1.1 責務

- Issue の一覧を表示する

#### 5.1.2 依存コンポーネント

- gorviz-server の HTTP API

### 5.2 gorviz-mcp

LLM エージェント向けの MCP[^mcp] サーバ。LLM エージェントとは stdio でやり取りする。

#### 5.2.1 責務

- 画面と同じ Issue と goroutine の診断を JSON で返す
- 事実と推測を確かさ[^certainty]のラベルで分けて返す (LLM が推測を事実として扱わないように)
- 読み取りだけを行う

#### 5.2.2 依存コンポーネント

- gorviz-server の HTTP API

## 6. gorviz package

アプリが import するライブラリ。agent の埋め込みとロガーアダプタからなり、どれを使うかは任意。

### 6.1 agent の埋め込み

ライブラリ型の agent。

#### 6.1.1 責務

- agent をアプリと同じプロセスの中で動かす
- execution trace を runtime の FlightRecorder[^flightrecorder] から取る。pprof の公開は要らない
- ロガーアダプタからログを受け取る
- server へのつなぎ方と送るものは gorviz-agent と同じ

#### 6.1.2 依存コンポーネント

- gorviz-server

### 6.2 ロガーアダプタ

アプリが使っているロガーを包み、ログを書くたびに gorviz の処理を挟む。

| ロガー | 包むもの |
| --- | --- |
| slog | ハンドラ |
| zap | Core と WriteSyncer |
| zerolog | Hook と Writer |
| logrus | Formatter |

#### 6.2.1 責務

- ログを書く goroutine の上で番号を振り、同じ番号を trace のログイベントとログ行の両方に書く。これでログ 1 行と、それを書いた goroutine を確実に結べる
- agent を埋め込んでいれば、出力したログ行をそのまま agent に渡す (slog だけは出力のバイト列を取り出せないので、JSON で書き直して渡す)
- ログの書式は変えない。番号のフィールドが 1 つ足されるだけ
- agent を別プロセスにする場合でも使える。使わなければ、ログの時刻から goroutine を推測する

#### 6.2.2 依存コンポーネント

- なし (agent を埋め込んでいれば、その agent にログを渡す)

## 7. gorviz-launcher

ローカルで server と agent を 1 つのプロセスでまとめて動かす。

### 7.1 責務

- server と agent を 1 つのプロセスで起動する。プロセスは 1 つでも別々のプログラムのままで、間は gorviz-server と gorviz-agent と同じく TCP でつなぐ
- launcher 自身は組み立てるだけで、通信の相手にはならない
- アプリを起動するコマンドを渡すと、アプリも子プロセスとして起動する。アプリの標準出力・標準エラー出力をそのまま端末に出しながら、ログとして取り込む
- アプリが終わると launcher も終わる
- コマンドを渡さなければアプリは起動しない。すでに動いているアプリのログは標準入力へ流す

### 7.2 依存コンポーネント

- gorviz-server
- gorviz-agent

## 8. ユーザーストーリー

| # | 誰が | いつ | 何をする | gorviz が返すもの |
| --- | --- | --- | --- | --- |
| 1 | 運用する開発者 | goroutine 数が増え続けているのに気づいた | Issues の `LEAK` から goroutine を開く | 生成元の go 文と止まっている場所 (file:line)・待機理由・件数の推移 |
| 2 | 運用する開発者 | 深夜のエラーに翌朝気づいた | Logs でそのエラーログの `Goroutine →` を押す | 保存済みの前後の trace のチャンクから、書いた goroutine の主因と止まっていた場所 |
| 3 | 運用する開発者 | 遅いリクエストを調べたい | 所要時間のあるログを選ぶ | そのリクエストの区間 (書いた時刻 − 所要時間 〜 書いた時刻) の内訳と主因 |
| 4 | 開発者 + LLM | 原因調査を LLM に任せたい | LLM が MCP でログを探し、goroutine の診断を取り、コードを読む | 確かさ付きの goroutine の診断を JSON で。LLM が原因と修正案を出す |
| 5 | 開発者 | 診断が本当か確かめたい / スケジューラを理解したい | Goroutine や Scheduler で再生する | GMP の概念図での再生 |

[^observation]: **観測データ**: agent がアプリから取って整形したもの。ログ・execution trace のチャンク・goroutine のスナップショット・リーク判定
[^leak]: **goroutine のリーク**: 終わるはずの goroutine が何かを待ったまま止まり、溜まり続けること
[^breakdown]: **状態別の時間内訳**: ある区間で、その goroutine がどの状態 (実行中・CPU 待ち・ロック待ちなど) にどれだけいたか
[^cause]: **主因**: 状態別の時間内訳のうち最も長いもの
[^certainty]: **確かさ**: `FACT` (runtime の判定や trace の記録から直接言える) と `INFERENCE` (gorviz が推移や比較から推し量った)
[^trace]: **execution trace**: goroutine ごとに「いつ走り、いつ待ち、いつ syscall に入ったか」をナノ秒単位で記録したもの。`go tool trace` で開く
[^dump]: **goroutine dump**: その瞬間の全 goroutine のスタックを並べたテキスト
[^goroutineleak]: **goroutineleak プロファイル**: Go 1.26 で入った、runtime 自身によるリーク判定。誰も起こせなくなった goroutine を GC の仕組みで見つけるので誤検知がない
[^annotation]: **annotation**: Kubernetes のオブジェクトに付けられるキーと値の組。Kubernetes 自身は中身を解釈せず、外のツールが設定を読み取るのに使う。Pod を選び出すための label とは別物
[^pprof]: **pprof**: Go 標準のプロファイリングの仕組み。HTTP 経由で trace やプロファイルを取れる
[^flightrecorder]: **FlightRecorder**: Go 1.25 で入った、trace の直近を常にメモリに持ち、必要なときに切り出せる機能
[^occurrence]: **発生**: 同じ Issue (種類と対象が同じもの) が起きた 1 回 1 回。インスタンスごとに、条件を満たし始めてから満たさなくなるまでを 1 回と数える。たとえば同じ場所の `CPU_WAIT_SPIKE` が 14 時と 18 時に起きたら、1 つの Issue に 2 回の発生がある。同じ時刻に 3 つのインスタンスで起きたら、3 回の発生と数える
[^chunk]: **チャンク**: execution trace を数秒ごとに切り分けた 1 つ分。agent が整形して server へ送り、server が持つ単位
[^gmp]: **GMP**: Go のスケジューラの仕組み。G (goroutine) を、P (実行の権利。GOMAXPROCS 個) を持った M (OS のスレッド) が走らせる
[^stw]: **STW (Stop The World)**: 全 goroutine を止めること。GC の一部や、全 goroutine のスタックを書き出す goroutine dump で起きる
[^markassist]: **mark assist**: 割り当ての多い goroutine が GC の作業を肩代わりさせられること。その間は本来の処理が進まない
[^mcp]: **MCP (Model Context Protocol)**: LLM エージェントが外部のツールやデータを呼び出すための共通の仕組み
