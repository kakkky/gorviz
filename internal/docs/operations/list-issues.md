# Issue の一覧 (種類・対象・確かさ・発生の回数・最後の発生・発生中か)

- `service` (必須)
- `from`・`to`
  - 期間 (RFC 3339)。省くと直近 31 日
- `cursor`・`max-bytes`
  - 続きの取り方。返る量で区切る

各行の `issue` を `get-issue` に渡す。
