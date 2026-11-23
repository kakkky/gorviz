# ログの検索 (期間・レベル・インスタンス・JSON のキーと値・文字列で絞る)

- `service` (必須)
- `from`・`to`
  - 期間。省くと直近 1 時間
- `level`
  - この重さ以上 (`DEBUG`・`INFO`・`WARN`・`ERROR`)
- `instance`
  - インスタンスで絞る
- `attr`
  - `key=value` で JSON のキーと値で絞る。複数渡せる
- `q`
  - メッセージと元の行に含まれる文字列
- `cursor`・`max-bytes`
  - 続きの取り方

各行の `log` を `get-log` に渡す。件数の当たりは先に `count-logs` で付ける。
