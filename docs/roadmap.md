# gorviz ロードマップ

[design.md](design.md) に対する進み具合。設計より頻繁に変わるので、ここに分けて持つ。

## できていること (PoC)

段階 1〜6 はひととおり PoC にした。どれも作りながら決めた部分を含み、本番での検証はまだ。

- **段階 1・2: 診断と Issue**
  - 画面: Overview・Issues・Logs・Scheduler・Goroutine (と、agent を選ぶ Agents)。G をクリックすると Goroutine に移る
  - Goroutine の診断: 状態別の時間内訳と主因 (`RUNNING` / `CPU_WAIT` / `BLOCKED` / `IO_WAIT` / `CGO` / `SLEEP` / `GC`)。録画の区切りをまたいで合計し、主因の状態で止まっていた場所 (file:line) を出す。GC (STW / mark assist) を取り込む。`CGO` はスタックの `runtime.cgocall` で見分ける
  - Issue の検出: `LEAK`・`ERROR_SPIKE`・`SLOW_REQUEST`・`CPU_WAIT_SPIKE`・`MEMORY_PRESSURE` (GC の頻度は `FACT`、ヒープが GC の目標値に張り付いているのは `INFERENCE`)
  - 確かさ `FACT` / `INFERENCE`。ログと G の結びつきは、slogtrace の番号なら `FACT`、その時刻に走っていた G が 1 つだけなら `INFERENCE`
  - ログの所要時間の読み取りと、リクエストの区間での Goroutine の表示
  - Goroutine から、その G を含む Issue と、時刻で絞った Logs に戻れる
  - 公式の呼び方 (local run queue など) と、P の syscall 状態の廃止
- **段階 3: 負荷をかけずに常に録る**
  - バイト数の上限つきのリングバッファ (`-trace-buffer-mib`)。今持てている区間を Overview に出す
  - 録り方の切り替え (`-record-mode always / intermittent`)、dump の間隔 (`-dump-interval`) と画面の更新 (`-refresh`) の分離
  - 負荷の実測 (下の「実測」)
- **段階 4: agent と server**
  - `cmd/agent` / `cmd/server` / `cmd/tui` / `cmd/cli` / `cmd/mcp` に分けた。ローカルでも agent と server は別プロセスで、`gorviz up` がまとめて起動する
  - agent から server へ外向きに TCP でつなぎ、その上で server が net/rpc の呼び出し側になる (pull)。軽いもの (ログ・dump・最新の区切り) は `-refresh` ごと、生の trace は保存するときと画面で見るときだけ運ぶ。最新の区切りは変わっていなければ運ばない
  - server が Issue をきっかけに前後の録画を保存する (`-save-dir`)。agent のリングバッファから消えたあとも、保存した録画から診断でき、`go tool trace` で開ける形で書き出せる
  - JSON の API。TUI・CLI・MCP は server とだけ話す
- **段階 5: 配置**
  - daemon 型: 1 つの agent で `-target` をカンマで並べ、対象ごとに別の agent として server につなぐ
  - sidecar 型: 同じバイナリを Pod / タスクに並べる (deploy/ に Kubernetes と ECS の例。実際のクラスタでは未検証)
  - ライブラリ型: `gorvizagent` をアプリに埋め込む。runtime の FlightRecorder から区切りごとに切り出し、前の区切りと重なる部分を落とす。区切りの間のすき間がほぼなくなる (実測で約 0.1ms。pprof 越しの agent は約 7ms)
- **段階 6: 広げる**
  - MCP サーバ (stdio。読み取りだけ)
  - goroutine を代わりに起動するライブラリ (kakkky/scope・errgroup・`sync.WaitGroup.Go`・`pprof.Do`) を飛ばした入口の関数と生成元
  - スコープの可視化: pprof label `scope` の付いた G をスコープごとにまとめる (Overview の Scopes)

## 実測

`go run ./example/overhead` (Apple Silicon、GOMAXPROCS=8、2026-11-15) の結果。CPU・channel・割り当て・ロックを混ぜた仕事を trace なし / ありで交互に回した。

- execution trace を取り続けたときの処理数の低下: 平均 2.4% (回ごとに -1〜6% とばらつく)。目安の 1〜2% より少し大きいが同程度
- goroutine dump (`debug=2`) にかかる時間: 待機中の G が 1,000 で 2.5ms、10,000 で 70ms、100,000 で 780ms。G 1 つあたり約 7µs で、Datadog の目安 (約 1µs) より重い。`debug=2` は全 G のスタックを文字にする間ずっと世界を止めるため

## 未着手・分かっていないこと

- **dump の負荷**: G が多い対象では `debug=2` の STW が長い。軽い形式 (`debug=1` や proto) に切り替えるか、リーク判定に要る分だけ `debug=2` で取る
- **agent と server の間の認証と暗号化**: 今は素の TCP で認証なし。server を対象と別の環境に置くときは必須
- **送る前に伏せる**: ログの機密を、agent から server に送る前、LLM に渡す前に伏せる仕組み
- **対象の見つけ方**: annotation (`gorviz.kakkky.dev/observe`) や docker label から対象を自動で見つける仕組み。今は `-target` を手で並べる
- **保存の量**: Issue が続くあいだ 1 分ごとに前後を保存するので、長く続く Issue でディスクが膨らむ。保持期間と上限
- **リモートの MCP**: server の隣に置く Streamable HTTP の MCP (今は stdio だけ)
- **kakkky/scope 側の計装**: scope がスコープの名前を pprof label `scope` に載せる (PoC では example が自分で `pprof.Do` で付けている)。寿命の保証・キャンセルの伝播の検証は未着手
- **Azure** (Container Apps など) がどの型に入るか (未調査)
- **動作確認で見えたこと**: `gorviz up` で動かした 1 回で、対象のプロセスごと約 80 秒止まり、その間の録画が抜けた (対象のログも止まっていた)。単独の agent で 2 分半回したときは起きなかった。原因は分かっていない
