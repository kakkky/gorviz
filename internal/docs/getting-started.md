# gorviz で調べ始める前に読む。何が分かり、どの順に操作を使うか

gorviz は Go のアプリの goroutine が「何に止まっていたか」を、Go ランタイムの execution trace から診断する。

- アプリ層の遅さ・エラーの増加に気づくのは APM の役目。gorviz はそこで見つけたログ・時間帯・Issue を入口に、goroutine と runtime を調べる
- 観測するアプリは「サービス」(同じアプリのインスタンスをまとめたもの) と「インスタンス」(プロセス 1 つ) の 2 段で扱う
  - サービス単位
    - Overview・Issues・Logs (list-issues・search-logs など)
  - インスタンス単位
    - goroutine の診断・状態の変化・集計 (diagnose-goroutine など)
- goroutine は「インスタンス + goroutine の ID」で区別する

## 使う順の目安

1. `list-services` でサービスとインスタンスを知る
2. `get-overview`・`list-issues`・`count-logs` で当たりを付ける
3. Issue の発生かログを入口にして `diagnose-goroutine` を呼ぶ。調べる期間は入口から server が決める
4. 返ってきた「次に調べる先」の ID をたどる (待ちを起こした goroutine・影響の大きかった goroutine など)
5. 返ってきた関数と file:line で手元のコードを読む。人に確かめてもらうときは GUI の URL を渡す

続けて読む: `main-causes`・`issue-kinds`・`certainty`・`investigation`・`data-retention`
