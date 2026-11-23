# goroutine の診断 (状態別の時間内訳・主因・止まっていた場所・リーク。確かさと何から算出したか付き)

- `service`・`instance`・`goroutine` (必須)
- 調べる期間は入口で決める。次のどれか 1 つを渡す
  - `log`
    - ログの ID。所要時間があればそのリクエストの区間、なければ書いた時刻の前後
  - `issue` と `occurrence`
    - Issue の発生の前後
  - `woken`
    - 起こされた側の goroutine の ID。その goroutine が待っていた区間 (待ちを起こした goroutine を調べるとき)
  - `from`・`to`
    - 期間をそのまま

返る `wokenBy` などの ID をたどって次を調べる。主因の意味は `main-causes`、確かさは `certainty` を読む。
