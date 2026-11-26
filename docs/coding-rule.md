# Coding Rule

層ごとの書き方の決まり。どこに何を置くか・依存の向きは [architecture.md](architecture.md)、振る舞いの仕様は [design.md](design.md) にある。ここに載っていないケースは、同じコンポーネントの近い層を読んで踏襲する。

## 組み立て

### cmd (`cmd/gorviz/`)

- `main` は、シグナルで終わる ctx を作り、`config.NewConfig()` で読んだ config を渡して `server.Run` か `cli.Run` を 1 回呼ぶだけ。cli に server として起動されたときが server、それ以外が cli
- `NewConfig`・`Run` がエラーを返したら、ログに出して `os.Exit(1)` する。起動・停止のエラーをログに出すのはここだけ

### エントリ (`internal/app/<コンポーネント>/<コンポーネント>.go`)

- `Run(ctx context.Context, cfg *config.Config) error` を 1 つだけ公開する。agent は「[agent](#agent)」の決まりに従う
- 各層の `NewXxx` を内側から順に呼んで、依存を引数で渡す。DI のライブラリは使わない
  - adapter が使う接続の driver は adapter より先に、presentation を受けて待ち受ける driver は presentation の後に作る
- コンストラクタ (`NewXxx`) の中で panic しない。失敗しうるなら error を返す
  - 組み立ては受けた error をそのまま `Run` から返し、起動を失敗させる
- driver の後始末は、コンストラクタが返す cleanup を `defer` で呼ぶ
- driver・worker の `Run` は、最後に `scope.Run` の下で起動する
- 組み立て以外の処理 (値の変換・分岐) は書かない。必要ならその層に置く
- 層をまたいでパッケージ名がぶつかるとき (driver の `socket` と presentation の `socket` など) は、エントリでだけ別名を付ける。ほかの層では別名を使わない

```go
// cmd/gorviz/main.go (server・cli のどちらか 1 つを呼ぶ)
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.NewConfig()
	if err != nil {
		slog.ErrorContext(ctx, "load config", slog.Any("err", err))
		os.Exit(1)
	}
	if err := <コンポーネント>.Run(ctx, cfg); err != nil {
		slog.ErrorContext(ctx, "run <コンポーネント>", slog.Any("err", err))
		os.Exit(1)
	}
}

// internal/app/<コンポーネント>/<コンポーネント>.go
func Run(ctx context.Context, cfg *config.Config) error {
	// driver (adapter が使う接続)
	conn, cleanup, err := xxxdriver.NewXxxConn(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	// adapter
	xxx := xxxadapter.NewXxx(cfg)
	yyy := yyyadapter.NewYyy(conn)

	// usecase
	uc := usecase.NewUseCase(xxx, yyy)

	// presentation
	xxxHandler := xxxhandler.NewXxxHandler(uc)

	// worker
	xxxWorker := worker.NewXxxWorker(cfg, uc)

	// driver (presentation を受けて待ち受ける)
	xxxListener := yyydriver.NewXxxListener(cfg, xxxHandler)

	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(xxxWorker.Run)
		sc.Go(xxxListener.Run)
		return nil
	})
}
```

## 並行処理

- agent には「[agent](#agent)」の決まりを当てる
- goroutine は [kakkky/scope](https://github.com/kakkky/scope) の下でだけ起動する。`go` 文を直接書かない
  - 親の ctx が終わったら子も終わり、子がすべて終わるまで親が戻らないようにするため。止まらない goroutine を残さない
- `sc.Go` に渡す関数は `func(ctx context.Context) error`。エラーを返すと同じ scope の兄弟も止まるので、続けられるエラーは返さない
- ctx の終わりで抜けたときは `nil` を返す。`ctx.Err()` をエラーとして返さない
- 共有する状態の排他は、状態を持つ driver の中で閉じる。usecase・adapter に mutex を持たせない

## config (`internal/app/<コンポーネント>/config/`)

- 設定は環境変数だけから読む。コマンドラインのフラグは使わない
  - cli のサブコマンドの引数・フラグは操作の入力で、設定ではない。presentation で受け取る
- `Config` は struct の tag で環境変数に対応づけ、[envconfig](https://github.com/kelseyhightower/envconfig) で読む

  ```go
  type Config struct {
  	Addr            string        `envconfig:"ADDR" default:"127.0.0.1:8080"`
  	ShutdownTimeout time.Duration `envconfig:"SHUTDOWN_TIMEOUT" default:"10s"`
  }

  func NewConfig() (*Config, error) {
  	var cfg Config
  	if err := envconfig.Process("", &cfg); err != nil {
  		return nil, err
  	}
  	return &cfg, nil
  }
  ```

- 無いと動けないものは `required:"true"`、無くても動けるものは `default` を付ける。既定値をコードの別の場所に書かない
- `*config.Config` を受け取ってよいのは、組み立て・driver・presentation・worker・adapter。usecase・domain には必要な値だけを渡す
  - usecase・domain のテストで、Config を丸ごと組まずに済ませるため

## driver (`internal/app/<コンポーネント>/driver/`)

- 状態や後始末を持つ資源 (接続・待ち受け・プロセス・メモリの置き場) を用意して起動する。外の API を呼ぶだけのクライアントは driver にせず、adapter の中で作る
- コンストラクタは `(*Xxx, func(), error)` を返す。後始末がないものは `(*Xxx, error)`
- 使えるかどうか (排他・形式) は、コンストラクタで確かめる。使えなければエラーを返して起動させない。使い始めてから気づかせない
- 待ち受けるものは `Run(ctx context.Context) error` を持ち、ctx が終わったら閉じて `nil` を返す
- 待ち受けは presentation を、接続は adapter を受け取る。driver から usecase を呼ばない
- テストのために driver を歪めない

## presentation (`internal/app/<コンポーネント>/presentation/`)

外から届いたものを受けて usecase を呼ぶ。手順も判断も持たない。

### 共通

- struct に、呼ぶ usecase と必要な config の値を持つ。`NewXxx(...)` で作る
- 1 つの入口は 1 つの usecase を呼ぶのを原則にする。画面のように複数をまとめて出すときだけ複数を許す
- usecase のほかにエントリから受け取ってよいのは、自分のコンポーネントを止める口 (ctx の cancel) だけ。server の停止を頼む API のため
- 受け取った形 (リクエスト・メッセージ・引数) から usecase の Input を組み、Output から返す形を組む。usecase の Output をそのまま外に出さない

### HTTP

- 命名は `<HTTPMethod><Resource>Handler`。返す相手 (画面・API) ごとにパッケージを分け、同じ名前にしてよい
- 1 ファイル 1 handler で、ファイル名は型の名前を snake_case にしたもの (`get_issue_handler.go`)。その handler だけが使う応答の型と ViewModel の組み立ても、同じファイルに置く
  - 複数の handler が使うもの (応答の書き出し・時刻の書き方・グラフの組み立てなど) は、何をするかが分かる名前のファイルに置く (`response.go`・`chart.go`)
- `ServeHTTP(rw http.ResponseWriter, r *http.Request)` を実装する
- path param は `r.PathValue(...)`、query は `r.URL.Query()` で取り出す
- エラーはリクエストのコンテキストに載せて即 return する。`http.Error` や status の書き込みを自分で書かない。middleware が defer で拾う
- Accept で返す形を選び分けるときは、handler ごとに書かず、共通の書き出しを通す
- 画面の ViewModel の組み立ては、handler の中の非公開の関数に切り出す。テンプレートに usecase の Output を渡さない

### middleware

- アクセスログ・エラーの返し方・panic の復旧だけを持つ。usecase を呼ばない
- エラーの取り出しと status への変換は defer で行う
- 適用の順序は、handler ごとに書かず、待ち受けに渡す側でまとめて組む

### socket

- `socket.SocketHandler` は、メッセージの種類ごとに 1 つの usecase を呼ぶ
- メッセージの読み書きと区切りは `protocol/agent` の関数を呼ぶ。presentation の中で形式を書かない
- エラーと panic は Handler の入口で拾い、接続を止めない。1 つのメッセージの失敗で、ほかのメッセージを読めなくしないため

### cli

- サブコマンド・引数・フラグは [urfave/cli](https://github.com/urfave/cli) (v3) で組む。`command.Command` はその定義と、各サブコマンドの Action を持つ
- 手順も判断も持たないので、presentation から adapter の `client.ServerClient` を直接呼ぶ。usecase を挟まない
- server の応答の中身は解釈・整形しない。そのまま出し、終了コードも server が載せた値を使う
- シナリオのファイルは、サブコマンドの入力として presentation で読む
- 利用者に見せる長い文言 (使い方・説明) はコードに直書きせず、埋め込んだドキュメントから引く

## worker (`internal/app/<コンポーネント>/worker/`)

- 命名は呼ぶ usecase の名前に合わせ、`Usecase` を `Worker` に替える (`CollectAppObservationsWorker`)
- `Run(ctx context.Context) error` を持ち、usecase の `Exec` を繰り返し呼ぶ。ctx が終わったら `nil` を返す
  - 起動したときに 1 回だけ呼び、その `Exec` が終わるまで待つ worker もある (`RunAppWorker`)。資源を `Exec` の中で持ち、呼び出しをまたいで持たないため
- 1 つの worker の `Exec` は重ねない。前の `Exec` が終わってから、間隔を待って次を呼ぶ
  - 間隔は `time.Ticker` で刻まず、`Exec` が終わるたびに待ち直す。重いときに間を空けずに回り続けて、負荷を詰まらせないため
  - 間を空けずに続けて呼ぶ worker (`Exec` の中で待つもの) は、間隔を 0 にする
- usecase のエラーで止まらない。ログに出して次の周期に進む。`Run` がエラーを返すのは、続けられないときだけ
- 1 回の `Exec` の中身 (何を対象に・何をするか) は usecase に書く。worker は「いつ呼ぶか」だけを持つ
- 1 つの worker は 1 つの usecase を呼ぶ
- 関心が同じ処理 (同じ相手から観測データを取る、など) は、1 つの worker・usecase にまとめる。関心の違う処理はまとめない
  - まとめた処理のあいだで間隔が違うときは、worker は短いほうの間隔で呼び、今回どれを行うかは usecase が domain に決めさせる

## usecase (`internal/app/<コンポーネント>/usecase/`)

- シグネチャは固定:

  ```go
  func (u *<Name>Usecase) Exec(ctx context.Context, in <Name>UsecaseInput) (<Name>UsecaseOutput, error)
  ```

- `Input` / `Output` は値型にする。pointer にしない。入力・出力がなくても空の struct を定義する
- エラーのときは `Output{}` (zero value) とエラーを返す
- usecase の依存は `usecase.go` の `UseCase` にまとめて持ち、`NewUseCase(...)` で受け取る。個々の usecase は `(us *UseCase) New<Name>Usecase()` で使う依存だけを取り出す
  - 依存を作るのに失敗しうるときは `(*<Name>Usecase, error)` を返す
- 何についての処理か (Issue の ID・期間など) は Input で受け取る。usecase の field に持たない
- ctx が終わったあとにも流す手順 (アプリを止めるなど) は、`context.WithoutCancel` で切れない ctx を作り、時間の上限を付けて流す
- 判断を含む処理は domain に書き、usecase から呼ぶ。usecase は読む・domain に渡す・書くの順番だけを持つ
- domain のエラーはそのまま返す。usecase で HTTP の status を意識しない

## domain (`internal/app/<コンポーネント>/domain/`)

- 判断を含む処理と型を置く。I/O をしない (ファイル・ネットワーク・時計を直接触らない)
  - 時刻は引数で受け取る。テストで時刻を固定するため
- 型のコンストラクタでバリデーションする。不正な状態の値を作らせない
- 値オブジェクトは `type X <primitive>` で表す。バリデーションが要るなら `NewX(...)` を用意する
- ファイルはドメインオブジェクトごとに切る。`<オブジェクト>.go` にオブジェクトと、それに属する値オブジェクトを置く
  - オブジェクトから導くもの・組み立てるものが大きくなったら、`<オブジェクト>_<関心>.go` に分ける (`observation_set.go`・`issue_detection.go`)
  - どのオブジェクトにも属さない境界の interface とエラーは、`accessor.go`・`client.go`・`errors.go` に置く
- 外部ライブラリは、形式を読むためのもの (`golang.org/x/exp/trace` など) だけを使う
- usecase が使う interface は、使う側の domain に置く。実装は adapter
  - interface は役割ごとに分け、使う操作だけを持たせる。1 つの大きな interface にまとめない
  - factory pattern (`repo.NewXxxRepository()`) は使わない。`NewUseCase` に役割ごとに渡す
  - interface のファイルの冒頭に `//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_<name>.go -package=mock` を置く

### エラー

- 種別は invalid argument / not found / internal の 3 つ。コンポーネントの domain に事前定義の変数で置く
- 種別に `.With(msg)` (メッセージだけ) か `.Wrap(cause, msg)` (原因付き) を被せて作る。生の Go のエラーを返さない
- 新しい種別をむやみに足さない
- internal の msg は、ログでどこで失敗したかがわかる短い英語を呼び出し箇所に直書きする (例 `ErrInternal.Wrap(err, "append log line")`)
- trace の末尾が欠けて出せないことは、エラーにしない。出せない欄として正常に返す

## adapter (`internal/app/<コンポーネント>/adapter/`)

- domain の interface を実装する。取る・送る・書く・読むまでで、判断はしない
  - 取ってきたものを読み解く (形式の解釈・値を決める) のは domain
- 外の型 (ライブラリの型・`protocol/agent` のメッセージの型) を domain に漏らさない。adapter の中で domain の型に詰め替える
- 外から返ってきたエラーは `ErrInternal.Wrap(err, "...")` で包む。見つからないことが外の応答でわかるときは `ErrNotFound` にする

### accessor

- 状態 (driver が持つメモリの置き場や trace、runtime が持つ coverage のカウンタや goroutine) を、domain の interface の操作に対応づける
- キーの決め方は accessor が持つ。usecase に組ませない

### client

- プロセスの外への送信と、外からの取得を持つ
- `NewXxxClient(...)` は `(*XxxClient, error)` を返す。失敗しうるかに関わらず形を揃える
- 接続先は、組み立てで config から渡す

## protocol (`internal/protocol/`)

- 契約を相手ごとに 1 パッケージで持つ。両側のコンポーネントが同じパッケージを使う
- 両側が揃えるもの (型・定数・線の上の読み書き) だけを置く。片側しか使わない振る舞い (server の起動・HTTP のクライアントなど) は、使う側の adapter に置く
- `server` は、server を起動するときの環境変数も持つ。起動する cli と起動される server が、同じ名前を使うため
- `agent` は、標準ライブラリだけで書く。agent と一緒にアプリのビルドに持ち込まれるため
- メッセージの種類を消すときは、その種類の番号を使い回さない。agent と server の版がずれても読み違えないようにするため

## agent

agent はアプリのモジュールの中でコンパイルされ、アプリと同じプロセスで動く。ほかのコンポーネントと同じ層に分けるが、次の決まりを足す。

### 使えるもの

- 標準ライブラリと `protocol/agent` だけを import する。`kakkky/scope`・envconfig も使わない
  - テストのファイルはアプリに overlay しないので、テストでは testify・gomock を使ってよい
- 設定は `os.Getenv` で読む

### 組み立てと並行処理

- アプリの main に overlay する import と defer が、agent の cmd に当たる。cmd は main の defer で `Stop()` を呼ぶだけ
- `Start()` はエントリの `init` から呼ぶ。main が agent を import するので、main パッケージの初期化より前に観測が始まる
- エントリは `Start()` と `Stop()` の 2 つだけを公開する。組み立てと driver・worker の起動を持ち、観測を始める・終えるのは `function.FunctionCallHandler` に渡す。エントリから usecase を直接呼ばない
  - `Start()` は観測が始まってから戻る。観測の始まりより後に、アプリの main を動かすため
  - `Stop()` は最後の観測データを送り終え、driver の後始末を済ませてから戻る
- goroutine は `go` 文で起動する。接続が閉じたら、すべての goroutine が終わるようにする
- server につながらなければ、何も起動せずに戻る

### アプリを止めない

- アプリの処理を止めない
  - アプリの呼び出しの中でブロックしない。渡し先が詰まっていたら待たない
  - 呼び出しごとの割り当てを増やさない
  - panic をアプリに伝えない。agent の中で復旧して、agent だけを止める
- アプリの終わり方を変えない。シグナルを横取りせず、`Stop()` の中で panic を握りつぶさない (main の panic はそのまま続ける)

## ログ

- コンポーネント自身のログは `log/slog` で出し、ctx と属性を渡す:

  ```go
  slog.InfoContext(ctx, "...", slog.String("phase", name))
  slog.ErrorContext(ctx, "...", slog.Any("err", err))
  ```

- 返すエラーは、その場でログに出さない。最後に受け取った側が 1 回だけ出す
  - HTTP のリクエストのエラーと panic は middleware が、socket のメッセージのエラーと panic は `socket.SocketHandler` が出す
  - worker の周期のエラーは worker が出す
  - 起動・停止のエラーは `main` が出す
  - その場で出すのは、エラーを処理して先に進むときだけ
- 外から受け取ったデータを丸ごとログに入れない。件数や大きさで書く

## 命名

- 中身を表さない名前 (`shared`・`common`・`util`・`types`・`helper`) のパッケージ・ファイルを作らない
- gorviz 自身は「コンポーネント」、観測される利用者のプログラムは「アプリ」と呼ぶ。名前でもこの区別を守る
- usecase は、何の何をどうするかが分かる名前にする。`<動詞><何の><何>Usecase` (`CollectAppObservationsUsecase`・`StreamAppRuntimeTraceUsecase`)
- usecase の Input / Output は usecase 名 + `Input` / `Output`
- 値オブジェクトの型名は単数形で、型 + 意味 (`IssueID`・`GoroutineID` など)
- パッケージ名は短い一語にし、何のものかは型名に含める (`kvs.InMemoryKVS`・`socket.SocketConn`・`accessor.IssueAccessor`)。対象の名前でパッケージを切らない (`issue.IssueAccessor` にしない)

## コメント

- 原則書かない
- 書くのは「なぜ」が非自明なとき (隠れた制約・ワークアラウンド・読み手が驚く挙動) だけ
- 「何をしているか」は命名で伝える
- design.md に書いてある仕様を、コメントで繰り返さない

## テスト

### 層ごと

- domain
  - 単体テスト。入力と出力で確かめる
  - 形式を読むものは、`testdata/` に置いた実物のファイルを入力にする
- usecase
  - adapter はすべて mock にする。メモリの置き場は driver の実物を使う
- adapter
  - accessor は、driver の実物のメモリの置き場を作って確かめる
  - client は相手に合わせて、`httptest`・`net.Pipe` を使う
- protocol
  - `agent` は、書いたメッセージが同じ値で読めることを確かめる
  - コンポーネント同士の通信は、両方を組み立てて確かめる (コンポーネントをまたぐ import の例外)
- driver・presentation
  - 書かない。組み立てて動かす確認で足りる。テストのために本体を歪めない

### 書き方

- テーブルテストにする。単発のケースでも次の形にする:

  ```go
  tests := []struct {
      name string
      // input / want / wantErr / mock などケースに応じて
  }{
      { ... },
  }
  for _, tt := range tests {
      t.Run(tt.name, func(t *testing.T) {
          t.Parallel()
          ...
      })
  }
  ```

- ケース名に挙動を平叙で書く。テストの中のコメントで補足しない
- testify の `require` (前提の失敗で即中断) と `assert` (続けて確かめる) を使い分ける
- エラーは `errors.Is(err, domain.ErrXxx)` で種別を比べる
- mock は `go.uber.org/mock` で生成し、`go generate ./...` で作り直す

### usecase の mock の登録

- 各ケースに `mock func(...)` を持たせ、その usecase が使う mock だけを引数で受ける
- `t.Run` の中で `gomock.NewController(t)` から mock を作り、`tt.mock(...)` で期待を登録してから usecase を作る
  - `NewUseCase` には、使わない依存を `nil` で渡す
  - メモリの置き場は driver の実物を作って渡す
- 呼ばれないことを確かめるケースは、`mock` を空の関数にする。登録していない呼び出しは gomock が失敗にする
- 引数は、テストで値がわかるものはその値で、usecase の中で組み立てるものは `gomock.Any()` で受ける
  - 組み立てた引数の中身は、`DoAndReturn` の中で `assert` で確かめる
- 呼ばれる回数が挙動の一部なら、`.Times(n)` で縛る
- ケースをまたいで使う値 (ctx・ID・返すエラー) は、`tests` の前に変数で置く。返すエラーは `errors.New("boom")` のように作り、`require.ErrorIs` で同じものが返ることを確かめる
- エラーのケースは、`require.ErrorIs` の後に Output が zero value であることを確かめて return する

```go
func TestXxxUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantErr := errors.New("boom")

	tests := []struct {
		name    string
		input   usecase.XxxUsecaseInput
		mock    func(a *mock.MockAAccessor, c *mock.MockCClient)
		want    usecase.XxxUsecaseOutput
		wantErr error
	}{
		{
			name:  "writes what it read and sends it",
			input: usecase.XxxUsecaseInput{ID: "id"},
			mock: func(a *mock.MockAAccessor, c *mock.MockCClient) {
				a.EXPECT().Read(ctx, domain.ID("id")).Return(item, nil)
				c.EXPECT().Send(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, m domain.Message) error {
					assert.Equal(t, "id", m.ID)
					return nil
				})
			},
			want: usecase.XxxUsecaseOutput{...},
		},
		{
			name:    "returns the read error without sending",
			input:   usecase.XxxUsecaseInput{ID: "id"},
			mock: func(a *mock.MockAAccessor, c *mock.MockCClient) {
				a.EXPECT().Read(ctx, domain.ID("id")).Return(domain.Item{}, wantErr)
			},
			wantErr: wantErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			a := mock.NewMockAAccessor(ctrl)
			c := mock.NewMockCClient(ctrl)
			tt.mock(a, c)

			uc := usecase.NewUseCase(a, nil, c).NewXxxUsecase()
			out, err := uc.Exec(ctx, tt.input)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, usecase.XxxUsecaseOutput{}, out)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
		})
	}
}
```
