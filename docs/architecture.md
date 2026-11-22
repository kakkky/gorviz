# Architecture

gorviz のコードの全体像。**どこに何があり、依存がどう流れ、観測データとリクエストがどう処理されるか** を扱う。図の名前は概念で、コードと一致するとは限らない。振る舞いの仕様は [design.md](design.md) にある。

この文書では、gorviz 自身の server・agent・cli・mcp・launcher を「コンポーネント」、gorviz が観測する利用者のプログラムを「アプリ」と呼ぶ (design.md と同じ)。

## リポジトリの構成

1 つの Go モジュールに、5 つのコンポーネント (実行ファイル) と、観測されるアプリが import する公開パッケージを置く。

公開パッケージ (`gorviz`・`logger/`) 以外はすべて `internal/` に置く。利用者が import できるのを公開パッケージだけに絞り、それ以外は互換を気にせず変えられるようにするため。

```
cmd/<コンポーネント>/      main (実行ファイルごと)
internal/
  app/                     コンポーネント
    agent/
    server/
    cli/
    mcp/
    launcher/
  domain/                  コンポーネントをまたぐ domain
    observation/           観測データ
  protocol/                コンポーネント同士の契約
    agentrpc/              agent と server
    serverapi/             server と cli・mcp
    loggerhook/            ロガーアダプタと agent
  pkg/                     gorviz に依らない道具
    funcname/
    histogram/
  docs/                    cli・mcp に埋め込むドキュメント
gorviz.go                  公開パッケージ (agent の埋め込み)
logger/gorviz<ロガー>/     公開パッケージ (ロガーアダプタ)
example/                   観測されるアプリの例
deploy/                    配置の例
```

## コンポーネントの境界

- コンポーネント同士は import しない。またぐものは `internal/` 直下に置く
  - 例外は launcher (server と agent を組み立てる) と、agent と server の通信のテスト
- 1 つのコンポーネントしか使わないものは、そのコンポーネントの層に置く
- 依存の向きは次の図のとおり。上が下を使い、下の段なら飛ばしてもよい。逆向きには使わない

```mermaid
flowchart TD
    gorviz["gorviz (公開)"]
    logger["logger/ (公開)"]
    subgraph internal["internal/"]
        app["コンポーネント (app/)"]
        protocol["契約 (protocol/)"]
        domain["コンポーネントをまたぐ domain (domain/)"]
        pkg["道具 (pkg/)"]
    end

    gorviz -->|"app/agent だけ (agent を埋め込む)"| app
    logger -->|"protocol/loggerhook だけ"| protocol
    app --> protocol --> domain --> pkg
```

- `logger/` は `gorviz` も `app/` も import しない。ロガーアダプタだけを使うアプリ (daemon 型・sidecar 型) に agent を持ち込まないため
- 中身を表さない名前 (`shared`・`common`・`util`・`types` など) の箱は作らない

### domain/

- 複数のコンポーネントが共通して使うドメイン知識を置く。型と、型の意味から決まる振る舞いだけで、判断の規則はそれを決めるコンポーネントの `domain/` に置く
- パッケージは中身の名前で切る。コンポーネントの `domain` と並べて import するときに別名を要らなくするため
- `observation` は、agent が作り server が読み解く観測データと、server がどこまで持っているかを表すカーソル。`agentrpc` の proto とほぼ 1 対 1 に対応する。生成型を使わないのは、domain を生成コードに依存させないため

### protocol/

- 契約を相手ごとに 1 パッケージで持つ
- `agentrpc` は gRPC の形と、`domain/observation` との詰め替え
- `serverapi` は HTTP API のクライアント。応答は解釈せず、そのまま返す
- `loggerhook` はプロセスの中の契約。ログ行と trace に書く番号の決まりと、ライブラリ型で埋め込んだ agent へ行を渡す口

### pkg/

- gorviz の仕様が変わっても変わらない道具。どこにも依存しない

## コンポーネントの中の層

どのコンポーネントも、`internal/app/<コンポーネント>/` の中を同じ層に分ける。ただし、手順も判断も持たない cli は presentation だけを、mcp は driver と presentation だけを持つ。層は外側から内側へ以下。

```mermaid
flowchart TD
    entry["&lt;コンポーネント&gt;.go (組み立て)"]
    driver["driver/"]
    presentation["presentation/"]
    worker["worker/"]
    usecase["usecase/"]
    domain["domain/"]
    adapter["adapter/"]

    entry --> driver
    entry --> presentation
    entry --> worker
    entry --> usecase
    entry --> adapter
    driver --> presentation
    presentation --> usecase
    worker --> usecase
    usecase --> domain
    adapter --> domain
```

- `<コンポーネント>.go` は組み立て (エントリ)。コンポーネントの直下に置く Go ファイルはこれ 1 つ
- `driver/` は状態や後始末を持つ資源 (接続・待ち受け・保存先) の用意と起動。待ち受けは presentation に、接続は adapter に渡す
- `presentation/` は外から届いたものを受けて usecase を呼ぶ入口
- `worker/` は定期的に動く、独立した処理。外から呼ばれず、自分で usecase を呼ぶ。同じプロセスの中で、エントリが起動する
- `usecase/` は処理の手順
- `domain/` は判定・診断・組み立てのロジック / 型 / usecase が使う interface
- `adapter/` は domain の interface の実装。外との接続だけを持つ (ディスク・外部への送信・pprof など)。取る・送る・書く・読むまでで、判断はしない。取ってきたものを読み解く・それで何かを決めるのは domain
- `config/` は設定の組み立て

- 依存の向きは図のとおり。矢印の先を使い、図にない向きでは使わない。`domain` はほかの層を使わない (使うのは `internal/domain/`・`pkg/` と、形式を読むためだけの外部ライブラリ (`golang.org/x/exp/trace` など) だけ)
- `presentation`・`worker` は usecase を呼ぶ側、`adapter` は domain の interface を通して usecase から呼ばれる側で、互いに依存しない
- interface は、使う側の `domain` に置く

### agent

```mermaid
flowchart TD
    subgraph driverIn["driver"]
        direction LR
        FileTailer["logsource.FileTailer"]
        TCPListener["logsource.TCPListener"]
        UDPListener["logsource.UDPListener"]
        JournaldReader["logsource.JournaldReader"]
        StdinReader["logsource.StdinReader"]
    end
    LoggerHook["protocol/loggerhook (ライブラリ型)"]
    subgraph presentation["presentation"]
        LogHandler["logs.Receiver"]
    end
    subgraph worker["worker"]
        direction LR
        SyncInstancesWorker
        ObservationWorker
        ExportWorker
    end
    subgraph usecase["usecase"]
        direction LR
        ForwardLogLineUsecase
        SyncInstancesUsecase
        CollectObservationsUsecase
        ExportObservationsUsecase
    end
    subgraph domain["domain"]
        direction LR
        TargetInstance
        Observation["observation.LogLine / TraceChunk / GoroutineSnapshot / GoroutineLeakCheck / Cursor"]
    end
    subgraph adapter["adapter"]
        direction LR
        KVSAccessor["accessor.KVSAccessor"]
        RingBufferAccessor["accessor.RingBufferAccessor"]
        RuntimeClient["client.PprofClient / client.FlightRecorderClient"]
        KubernetesClient["client.KubernetesClient"]
        ServerClient["client.GorvizServerClient"]
    end
    subgraph driverOut["driver"]
        direction LR
        InMemoryKVS["kvs.InMemoryKVS"]
        RingBuffer["ringbuffer.RingBuffer"]
        ClientConn["grpcconn.ClientConn"]
    end

    driverIn --> LogHandler
    LoggerHook --> LogHandler
    LogHandler --> ForwardLogLineUsecase
    SyncInstancesWorker --> SyncInstancesUsecase
    ObservationWorker --> CollectObservationsUsecase
    ExportWorker --> ExportObservationsUsecase
    usecase --> domain
    domain -. 実装 .-> adapter
    KVSAccessor --> InMemoryKVS
    RingBufferAccessor --> RingBuffer
    ServerClient --> ClientConn
```

### server

```mermaid
flowchart TD
    subgraph driverIn["driver"]
        direction LR
        GRPCServer["grpcserver.GRPCServer"]
        HTTPServer["httpserver.HTTPServer"]
    end
    subgraph presentation["presentation"]
        direction LR
        Interceptor["interceptor.Interceptor"]
        ObservationService["observation.ObservationService"]
        Middleware["middleware.Middleware"]
        Handler["web.Handler (+ Templ)"]
        APIHandler["api.Handler"]
    end
    subgraph worker["worker"]
        direction LR
        AggregateMetricsWorker
        AggregateGoroutineStateBreakdownsWorker
        DetectIssuesWorker
        PreserveExecutionTracesAroundErrorLogsWorker
        AttachLogWriterGoroutinesWorker
        PruneDataWorker
    end
    subgraph usecase["usecase"]
        direction LR
        IngestObservationsUsecase
        AggregateMetricsUsecase
        AggregateGoroutineStateBreakdownsUsecase
        DetectIssuesUsecase
        PreserveExecutionTracesAroundErrorLogsUsecase
        AttachLogWriterGoroutinesUsecase
        DiagnoseGoroutineUsecase
        GetOverviewUsecase
        ListIssuesUsecase
        ListLogsUsecase
        ListTraceChunksUsecase
        PruneDataUsecase
    end
    subgraph domain["domain"]
        direction LR
        Metric
        Issue
        Goroutine["Goroutine (StateBreakdown / Diagnosis / Leak)"]
        Observation["observation.LogLine / TraceChunk / GoroutineSnapshot / GoroutineLeakCheck / Cursor"]
    end
    subgraph adapter["adapter"]
        direction LR
        DiskAccessor["accessor.LogAccessor / TraceAccessor / SnapshotAccessor / IssueAccessor / ..."]
        NotifyClient["client.WebhookClient / SlackClient / MailClient"]
    end
    subgraph driverOut["driver"]
        Root["filesystem.Root"]
    end

    GRPCServer --> Interceptor --> ObservationService --> IngestObservationsUsecase
    HTTPServer --> Middleware
    Middleware --> Handler
    Middleware --> APIHandler
    Handler --> GetOverviewUsecase
    Handler --> ListIssuesUsecase
    Handler --> ListLogsUsecase
    Handler --> ListTraceChunksUsecase
    Handler --> DiagnoseGoroutineUsecase
    APIHandler --> GetOverviewUsecase
    APIHandler --> ListIssuesUsecase
    APIHandler --> ListLogsUsecase
    APIHandler --> ListTraceChunksUsecase
    APIHandler --> DiagnoseGoroutineUsecase
    AggregateMetricsWorker --> AggregateMetricsUsecase
    AggregateGoroutineStateBreakdownsWorker --> AggregateGoroutineStateBreakdownsUsecase
    DetectIssuesWorker --> DetectIssuesUsecase
    PreserveExecutionTracesAroundErrorLogsWorker --> PreserveExecutionTracesAroundErrorLogsUsecase
    AttachLogWriterGoroutinesWorker --> AttachLogWriterGoroutinesUsecase
    PruneDataWorker --> PruneDataUsecase
    usecase --> domain
    domain -. 実装 .-> adapter
    DiskAccessor --> Root
```

- `filesystem.Root` は保存先を開くときに、ロックを取り、形式のバージョンを確かめ、書きかけの一時ファイルを消す。一時ファイルに書いてから名前を付ける書き込みも Root が持つ
- 追記するファイルの途中で切れた末尾は、accessor がそのファイルに追記を始める前に切り詰める
- 層の外に `assets/` (CSS・JS・Stimulus controllers) を持つ

### cli

server の HTTP API を呼んでそのまま返すのと、`internal/docs/` を引くだけで、手順も判断も持たない。presentation だけを持つ。

```mermaid
flowchart TD
    subgraph presentation["presentation"]
        Command["command.Command"]
    end
    ServerAPIClient["protocol/serverapi.Client"]
    Docs["internal/docs"]

    Command --> ServerAPIClient
    Command --> Docs
```

### mcp

cli と同じく手順も判断も持たない。stdio で MCP を待ち受ける driver と、presentation だけを持つ。

```mermaid
flowchart TD
    subgraph driver["driver"]
        MCPServer["mcpserver.MCPServer (stdio)"]
    end
    subgraph presentation["presentation"]
        ToolHandler["handler.ToolHandler (+ server instructions)"]
    end
    ServerAPIClient["protocol/serverapi.Client"]
    Docs["internal/docs"]

    MCPServer --> ToolHandler
    ToolHandler --> ServerAPIClient
    ToolHandler --> Docs
```

### launcher

組み立てと config だけを持つ。server と agent を同じプロセスで並べて呼び、間は TCP (localhost) でつなぐ。launcher 自身は通信の相手にならない。

## 処理の流れ

### 観測データを取る (agent)

```mermaid
sequenceDiagram
    participant W as ObservationWorker
    participant U as CollectObservationsUsecase
    participant KVS as accessor.KVSAccessor
    participant RC as client.PprofClient
    participant D as domain
    participant RB as accessor.RingBufferAccessor

    loop チャンクごと
        W->>U: Exec
        U->>KVS: 観測するインスタンスの一覧を読む
        par インスタンスごと
            U->>RC: 生の execution trace・プロファイルを取る
            U->>D: チャンク・スナップショットを組み立てる
            U->>RB: インスタンスのキーに積む
        end
    end
```

### ログを取り込む (agent)

```mermaid
sequenceDiagram
    participant S as logsource.FileTailer など
    participant R as logs.Receiver
    participant U as ForwardLogLineUsecase
    participant D as domain
    participant RB as accessor.RingBufferAccessor

    S->>R: 1 行
    R->>U: Exec
    U->>D: 行を読み取り、どのインスタンスのものか決める
    U->>RB: インスタンスのキーに積む
```

### 観測データを送る (agent → server)

```mermaid
sequenceDiagram
    box agent
        participant W as ExportWorker
        participant U as ExportObservationsUsecase
        participant RB as accessor.RingBufferAccessor
        participant C as client.GorvizServerClient
    end
    box server
        participant G as grpcserver.GRPCServer
        participant I as interceptor.Interceptor
        participant O as observation.ObservationService
        participant RU as IngestObservationsUsecase
        participant A as accessor (disk)
    end

    loop 数秒ごと
        W->>U: Exec
        loop インスタンスごと
            U->>RB: カーソルの続きを取り出す
            U->>C: 送る (インスタンスの名前・カーソル付き)
            C->>G: gRPC (grpcconn の接続の上)
            G->>I: トークンを確かめる
            I->>O: Export (gRPC)
            O->>RU: Exec
            RU->>A: 抜けと重複を確かめ、書く
            RU-->>C: server のカーソル
            C-->>U: server のカーソル
            U->>RB: カーソルを覚える
        end
    end
```

### 観測データから作る (server)

server が観測データから作るものは、作るものごとに worker を分ける (`AggregateMetricsWorker`・`AggregateGoroutineStateBreakdownsWorker`・`DetectIssuesWorker`・`PreserveExecutionTracesAroundErrorLogsWorker`・`AttachLogWriterGoroutinesWorker`)。どの worker も、チェックポイントの続きから作る。

```mermaid
sequenceDiagram
    participant W as AggregateMetricsWorker など
    participant U as AggregateMetricsUsecase など
    participant A as accessor (disk)
    participant D as domain

    loop worker ごとの間隔
        W->>U: Exec
        U->>A: チェックポイントを読む
        U->>A: その続きの観測データを読む (材料がそろった分だけ)
        U->>D: 集計する・判定する・突き合わせる
        U->>A: 結果を書く
        U->>A: チェックポイントを進める
    end
```

### Issue の検出と通知 (server)

```mermaid
sequenceDiagram
    participant W as DetectIssuesWorker
    participant U as DetectIssuesUsecase
    participant A as accessor (disk)
    participant D as domain
    participant N as client.WebhookClient など

    loop 数秒ごと・5 分ごと
        W->>U: Exec
        U->>A: チェックポイントを読む
        U->>A: その続きの観測データを読む
        U->>D: 閾値・普段との比較で判定する
        U->>A: Issue と、前後の execution trace を書く
        U->>N: 発生が始まったら通知する
        U->>A: チェックポイントを進める
    end
```

### GUI と HTTP API のリクエスト (server)

```mermaid
sequenceDiagram
    participant B as ブラウザ / cli / mcp
    participant H as httpserver.HTTPServer
    participant M as middleware.Middleware
    participant P as web.Handler / api.Handler
    participant U as ListIssuesUsecase など
    participant A as accessor (disk)

    B->>H: HTTP
    H->>M: ログイン・トークンを確かめる
    M->>P: リクエスト
    P->>U: Exec
    U->>A: 読む
    U-->>P: 結果
    P-->>B: HTML (Templ) / TOON・JSON (Accept)
```

### gorviz-cli

```mermaid
sequenceDiagram
    participant L as LLM エージェント / 人
    box cli
        participant E as エントリ
        participant P as command.Command
        participant C as protocol/serverapi.Client
    end
    box server
        participant M as middleware.Middleware
        participant S as api.Handler
    end

    L->>E: サブコマンドを実行する (シェル)
    E->>E: 環境変数からトークンを読む
    E->>C: トークンを持たせて作る
    E->>P: 実行する
    P->>C: 呼ぶ (Accept: TOON)
    C->>M: HTTP API (トークン付き)
    alt トークンが正しい
        M->>S: リクエスト
        S-->>C: TOON
        C-->>P: そのまま
        P-->>L: そのまま出力する
    else トークンがない・正しくない
        M-->>C: 401
        C-->>P: 認証のエラー
        P-->>L: トークンの渡し方を示して終わる
    end
```

### gorviz-mcp

```mermaid
sequenceDiagram
    participant L as LLM エージェント
    box mcp
        participant E as エントリ
        participant D as mcpserver.MCPServer
        participant P as handler.ToolHandler
        participant C as protocol/serverapi.Client
    end
    box server
        participant M as middleware.Middleware
        participant S as api.Handler
    end

    E->>E: 環境変数からトークンを読む
    E->>C: トークンを持たせて作る
    E->>D: stdio で待ち受ける
    L->>D: tool の呼び出し (stdio)
    D->>P: tool
    P->>C: 呼ぶ (Accept: TOON)
    C->>M: HTTP API (トークン付き)
    alt トークンが正しい
        M->>S: リクエスト
        S-->>C: TOON
        C-->>P: そのまま
        P-->>D: tool の結果
    else トークンがない・正しくない
        M-->>C: 401
        C-->>P: 認証のエラー
        P-->>D: tool のエラー (トークンの渡し方を示す)
    end
    D-->>L: tool の結果
```

## サービスとインスタンス

- server の usecase は server 全体で 1 つ。どのサービス・インスタンスのものかは、usecase の入力で渡す
- サービスとインスタンスの区切りは、adapter のディスクの置き場 (`services/<サービス>/service/`・`services/<サービス>/instances/<インスタンス>/`) が持つ
- 観測データは、ディスクに書いてからディスクから読む。メモリに置くのは、送るまで・書くまでの分と、つながっている agent の状態だけ
- どこまで持っているか (カーソル) も、ディスクに書いてある最後の分から引く

## 横断関心事

認証・アクセスログ・エラーの返し方・panic の復旧は、各層に書かず、入口の手前でまとめて扱う。

- HTTP (GUI・HTTP API) は、`presentation/middleware` で扱う
- gRPC (agent からの観測データ) は、`presentation/interceptor` で扱う
- driver には置かない。エラーの種別 (domain) や画面 (view) を使うため
- handler・usecase は、認証やエラーのレスポンスを自分で書かない

### エラーフロー

各層は生の Go エラーを返さない。domain のエラー種別に包んで返す。種別は「invalid argument / not found / internal」の 3 つ。種別はコンポーネントごとの `domain` に置く。

HTTP の handler は、エラーをリクエストのコンテキストに載せて即 return する。outer の middleware が defer で取り出し、種別を status に変えて返す。

- invalid argument は 400、not found は 404、internal は 500
- GUI はエラーの画面を、HTTP API は Accept に応じた形 (TOON / JSON) のエラーを返す
- HTTP API の not found には、打ち間違いの候補やあるものを添える (design.md「LLM エージェントの利用に特化するためのポイント」)
- panic は同じ middleware の defer で復旧し、internal として扱う

gRPC は、interceptor が種別を gRPC の status (InvalidArgument / NotFound / Internal) に変える。panic も interceptor で復旧する。

保持期間を過ぎて出せないことは、エラーにしない。出せない欄として正常に返す。
