# PoC を運用に足るものにするために

PoC を、作り手が答えを知らない不具合と負荷の下で動かして分かった、運用に足りないところ。仕様は [design.md](../design.md) にある。ここに書くのは、今の実装を実際に動かして見えた問題と、その直し方の方向。

## 目次

- [どう確かめたか](#どう確かめたか)
- [当たったもの](#当たったもの)
- [データの取り込みと保存](#データの取り込みと保存)
- [問い合わせ](#問い合わせ)
- [観測されるアプリへの負担](#観測されるアプリへの負担)
- [Issue の判定](#issue-の判定)
- [診断](#診断)
- [直す順番](#直す順番)

## どう確かめたか

チュートリアルの todo-app ではなく、別のアプリに agent をライブラリ型で埋め込み、次の不具合を入れて、平常のリクエストと同時に流した。

- 誰も close しない channel を `range` で待ち続ける goroutine (受け手側のリーク)
- 設定の再読み込みが `RWMutex` の `Lock` を 1.2 秒持ち、その間 `RLock` が待たされる
- 外部の HTTP サービスが 700ms かかる
- 枠 2 つのセマフォ (buffer つき channel) に 20 の同時リクエストが並ぶ
- `runtime.LockOSThread` してから寝る goroutine を 200 本作る

負荷は 2 段階で流した。

- 約 2 万 rps (リクエストごとにログを 1 行書く)
- 約 400 rps

server・agent・負荷をかける側は、すべて同じ 8 コアのマシンで動かした。数字は目安。

## 当たったもの

400 rps では、診断の中身はおおむね正しかった。

- `GOROUTINE_LEAK` が `FACT` で出た。300 of 300 と、止まっている file:line まで正確
- `LOCK_CONTENTION` が出た。待ちの 99.8% が `sync.(*RWMutex).RLock` を呼んだ行に寄った
- セマフォで待つ G は、主因 `SYNC_WAIT` (`chan send`) と待った行が出た。woken by に、枠を返した側の G が出た
- 生成元ごとの集計で、スレッドを固定して寝ている G が 200 本・`SLEEP` と出た
- ログ 1 行から、書いた G へ `FACT` で辿れた

## データの取り込みと保存

- 負荷の高い時間帯の execution trace が丸ごと欠ける。2 万 rps を流した 3 分間の trace は残らず、その間の診断はできなかった。agent の送信は deadline を超え続け、server の 1 回の受け取りは最大 10 秒かかった
- 欠けたことが利用者に見えない。残っているデータの一覧に穴があるだけで、なぜ欠けたか (agent が捨てたのか、送れなかったのか) は分からない
- server の周期の処理が、ログの量に比例して重くなる。期間内のサービスのログを全部メモリに読み、JSON をデコードしてからインスタンスで絞っている (`internal/app/server/usecase/preserve_execution_traces_around_error_logs_usecase.go:50` の `instanceLogs`、`internal/app/server/usecase/aggregate_metrics_usecase.go:70`)。2 万 rps の 3 分のあと、server は RSS 6GB に達し、10 分たっても CPU 140% で追いつかなかった
- 処理が溜まった server は SIGTERM で止まらなかった

## 問い合わせ

- 問い合わせのたびに execution trace を読み直して計算するので、期間に比例して重い。400 rps のアプリ 1 つでも、`summarize-goroutines` は 20 秒の期間なら返るが、1 分の期間では server が CPU 150% で 8 分以上計算し続けて返らなかった (`SummarizeGoroutinesUsecase.Exec`)
- 問い合わせに時間の上限がなく、呼んだ側が諦めても server は計算を続ける。重い問い合わせが重なると、ほかの問い合わせや周期の処理まで遅れる

## 観測されるアプリへの負担

- 400 rps 弱で、agent を入れるとアプリの CPU が約 2.9 倍 (60 秒あたり 3.2 秒 → 9.4 秒)、RSS が 22MB → 245MB になった。レイテンシ (p50・p99) は誤差の範囲だった
- server に送れない間は、agent がアプリのメモリに溜める。server を落とすと、アプリの RSS は 295MB → 659MB まで増えて頭打ちになった。2 万 rps のときは、アプリの heap が一時 1.7GB まで膨らんだ
- server を落としても、アプリのレイテンシは変わらなかった
- ロガーアダプタの Mutex (`logger/gorvizslog/gorvizslog.go:34`, `:36`) に、ログを書くすべてのリクエストが並ぶ

## Issue の判定

- agent 自身の goroutine を、アプリの問題として数える。負荷のあとの `CPU_CONTENTION` で、実行待ちが長かった G の上位が agent のもの (スナップショットの作成・gRPC の送信・trace の解析) だった
- 閾値が低く、平常時にも出る
  - `LONG_STW` は 1 回 1ms で出る。起動直後の 1.18ms で出て、負荷のあとにも 15 回出た。agent がスナップショットを取るときの stop-the-world が効いているように見える (確かめてはいない)
  - `CPU_CONTENTION` は RUNNABLE から RUNNING までの p99 が 1ms で出る。負荷のかかったサーバーではいつも越える
- `THREAD_EXPLOSION` は上限の半分を超えるか、過去の同じ曜日・時刻と比べて多いかで出る。過去との比較は、同じ曜日の履歴が 1 週分そろうまで動かないので、入れて最初の 1 週は上限の半分を超えるまで気づけない

## 診断

- `RWMutex` の待ちでは、ロックを持って待たせていた側 (held by) が出ない。`Mutex` では出る。根本の原因 (`Lock` を持ったまま遅い処理をしている側) に辿れない
- 外部の HTTP を待つリクエストの G は、主因が `IO_WAIT` ではなく `SYNC_WAIT` (`net/http.(*persistConn).roundTrip` の `select`) になる。ランタイムから見れば正しい。ただ、いちばんよくある「外が遅い」が goroutine 同士の待ち合わせに見える。woken by の `readLoop` を開けば `IO_WAIT` に辿れるが、1 手余計にかかる
- ログから開く G の期間はログの前後で決まるので、keep-alive の接続を受け持つ G では、同じ G が処理した前後の別のリクエストまで内訳に混ざる

## 直す順番

1. server のデータの持ち方を変える。周期の処理は checkpoint より後だけを読む。ログはインスタンスごとに分けて置き、時刻で引けるようにする。goroutine の集計など重い計算はチャンクが届いたときに済ませ、問い合わせでは結果を読むだけにする。問い合わせには時間の上限を付け、呼んだ側が諦めたら止める
2. agent が溜める量に上限を付け、上限を超えて捨てたことを server に伝えて、欠けた理由を画面と API に出す。平常時の +220MB の内訳 (FlightRecorder の保持量・送信待ちの量など) を測り、減らせるところを減らす
3. agent 自身の goroutine を判定から外す。`LONG_STW` と `CPU_CONTENTION` の閾値を、平常時には出ない値にする
4. `RWMutex` でも held by を出す。`net/http` のクライアントの待ちは、待ち先の `readLoop` の `IO_WAIT` まで畳んで見せる
5. ここまで直したら、実際のトラフィックのあるサービス (kakkky.dev など) に 1〜2 週置き、欠け・誤検知・負担を見る。過去との比較による判定も、その間にやっと動き出す
