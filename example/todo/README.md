# gorviz チュートリアル: Todo アプリの不具合を調べる

`docker compose up` (か `make launcher.up`) で起動した状態から始める。

- Todo アプリ: http://localhost:8080
- gorviz: http://localhost:7070

裏で仮想ユーザーが 3 人動いているので、触らなくてもリクエストは流れている。
シナリオは 1 つずつ試す。答え (原因のコード) は各シナリオの最後に書いてある。

gorviz の Issues が見つけるのは runtime の問題 (LEAK・CPU_WAIT_SPIKE・MEMORY_PRESSURE) だけ。遅いリクエストやエラーの増加に気づくのは APM (Datadog など) の役目で、
本番ではそこで見つけた時間帯やログ (trace_id など) を手がかりに gorviz に入る。シナリオ 2・3 はその流れで、APM の代わりに Todo アプリの画面とログで異常に気づく。

## 0. 平常時を見ておく

1. gorviz の **Overview** を開く
   - 右上で見る期間を選ぶ (15m〜30d)。上に「showing 何時〜何時」と、集計を始める前の部分は no data と出る
   - 「jump to a time」で、時刻か Datadog / Grafana の URL を入れると、その時間帯に合わせられる
   - 上段は今の様子: open issues (上位 3 件) と、最新の dump の G の数と状態
   - 下段は選んだ期間の推移で、すべて同じ横軸: Goroutines (状態ごとの積み上げ棒)・Heap (live heap と heap goal)・GC・Mark assist・Waiting for P
   - グラフに hover すると、全グラフの同じ時刻の値が出る。ドラッグで拡大できる
   - Top origins は、どの go 文の G が多いか (今と、期間中の最大)。Goroutines の棒に hover すると、その時刻に多かった生成元が出る
2. **Scheduler** を開く
   - P のカードに、今走っている G が出る。ほとんどの P は idle のはず
   - ▶ で出来事を 1 つずつ進められる

## 1. goroutine が増え続ける (LEAK)

1. Todo アプリで、**リマインダー** にチェックを入れて 5〜10 回追加する
   - ときどき「reminder could not be registered (timeout)」が出る
2. gorviz の **Issues** に `LEAK FACT main.(*Notifier).Register @ todo/services.go:26` が出る (dump 2 回分、10 秒ほどかかる)
   - trend の線は、追加するたびに右肩上がりになる
3. 行の右の **G 番号** をクリックして、Goroutine 画面に進む
   - stopped in: `main.(*Notifier).Register.func1`
   - wait reason: `chan send` (送れずに止まっている)
   - created by: `main.(*Notifier).Register @ todo/services.go:26`
   - LEAK FACT: runtime が「この channel に届く G はもういない」と判定した
4. 考える: なぜ送り先がいないのか

**答え**: `services.go:24-37`。結果を buffer のない channel に送るが、1 秒のタイムアウトで受け手 (`select`) が先に返ると、送り手は永久に止まる。`make(chan error, 1)` にすれば直る。

## 2. ときどきリクエストが遅い

1. Todo アプリで **今すぐバックアップ** を押し、すぐ別のタブで一覧を開く (1 分ごとに自動でも起きる)
   - 一覧の表示に 1 秒以上かかる。本番なら Datadog で「この時間帯に p99 が跳ねた」と気づくところ
2. gorviz の **Logs** を開き、その時間帯に絞る (from / to に時刻を入れる)。duration が 1 秒を超えた行を順にクリックする。同じ時刻に 2 種類の遅い G がいる
   - 待たされた側 (一覧など): 上のカードの内訳が **BLOCKED** でほとんど埋まる
   - 待たせた側 (バックアップのリクエスト): 内訳が **SLEEP** でほとんど埋まる
3. 待たされた側で **diagnose →** を押し、Goroutine 画面に進む
   - Diagnosis: `BLOCKED` / stopped in: `sync.(*Mutex).Lock` / wait reason: `sync`
   - GMP replay の右の **waiting** に `net/http.(*conn).serve` (`sync`) の G が何個も並んでいる → 1 つのロックをみんなで待っている
4. 待たせた側でも **diagnose →** を押す
   - Diagnosis: `SLEEP` / stopped in: `time.Sleep` → ロックの中で寝ている
5. 行の右の **overview** を押すと、その前後 5 分の Overview に移る。Waiting for P も Mark assist も平らなままで、CPU や GC のせいではないと分かる
6. 考える: 誰が、なぜロックを長く握っているか

**答え**: `services.go:70-85`。バックアップはストアのロック (`store.mu`) を握ったまま、1.5 秒かかる書き出しをする。ロックの中では `json.Marshal` までにして、書き出しはロックの外でする。

> いまの gorviz の弱いところ: 止まった場所が `sync.(*Mutex).Lock` や `time.Sleep` (標準ライブラリ) で、自分のコードの行 (`store.go` の `List`、`services.go` の `Backup.Run`) が出ない。待たされた G と待たせた G のつながり (誰がロックを握っていたか) も、自分で突き合わせるしかない。

## 3. エラーが急に増える

1. Todo アプリで未完了の todo を **完了** にする。2 分のうち 30 秒は「mail could not be sent」になる
   - 仮想ユーザーも完了にしているので、待っていても出る。本番なら Datadog でエラー率の山に気づくところ
2. gorviz の **Logs** で level を **error** にする
   - 上の件数の帯が、2 分ごとに赤く山になる
   - エラーの中身: `mail service unavailable (503)`
3. ログの行をクリックし、書いた G の内訳を見る
   - ほとんどが IO_WAIT と SLEEP (外の呼び出しを待っている時間) で、BLOCKED はほぼない → goroutine 同士の待ち合わせの問題ではない
4. 考える: 自分のコードの問題か、外の問題か

**答え**: `services.go:43-52`。外部のメール送信サービスが周期的に落ちている。gorviz では「goroutine は止まっておらず、外の呼び出しが失敗している」と切り分けられる。

## 4. CPU が足りない (CPU_WAIT_SPIKE)

1. Todo アプリで **サンプルを 1000 件追加** を 4〜5 回押す
2. **統計を見る** を何度か押す (1 回 1 秒ほどかかる)
3. 押した直後に gorviz を見る
   - **Issues**: `CPU_WAIT_SPIKE FACT N goroutines waiting for a P on average`
   - **Scheduler**: すべての P のカードが埋まり、local run queue に `main.Stats.func1` の G が並ぶ
   - **Overview**: Waiting for P のグラフが跳ね上がる (Issues から消えたあとも、推移には残る)
4. 考える: なぜ P が足りなくなるのか

**答え**: `services.go:109-125`。todo ごとに goroutine を立てて重いハッシュを計算するので、件数の数だけ実行待ちの G ができ、他のリクエストも P を待たされる。worker の数を P の数までに絞るか、結果をキャッシュする。

> いまの gorviz の弱いところ: CPU_WAIT_SPIKE は直近 2 秒の録画だけで判定するので、数秒で Issues から消える。

## 5. GC が多すぎる (MEMORY_PRESSURE)

1. シナリオ 4 のあと (todo が数千件ある状態で)、**CSV エクスポート** を何度も押す
2. 押した直後に gorviz を見る
   - **Issues**: `MEMORY_PRESSURE FACT N GC cycles per second`
   - **Overview**: GC と Mark assist のグラフが跳ね上がる。live heap は平らなまま (残るメモリは増えておらず、一時的な確保が多い)
3. 押した時刻の前後の Logs を見て、そのとき何のリクエストが走っていたかを探す (`/export.csv`)
4. 考える: 何がそんなにメモリを確保しているのか

**答え**: `services.go:141-147`。1 行足すたびに CSV 全体の文字列を作り直すので、件数の 2 乗のメモリを確保する。`strings.Builder` か `encoding/csv` で書く。

> いまの gorviz の弱いところ: CPU_WAIT_SPIKE と同じく、数秒で Issues から消える。Issue の G 番号 (GC の手伝いを一番長くさせられた G) に進んでも、内訳が空のことがある。どの関数がメモリを確保したかも出ない。
