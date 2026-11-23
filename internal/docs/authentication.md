# server へのつなぎ方 (トークンと URL の渡し方)

- トークンは環境変数 `GORVIZ_TOKEN` で渡す。対話の要る操作は持たない
- server の URL は環境変数 `GORVIZ_SERVER_URL` で渡す (例 `https://gorviz.internal:7070`)
- server が自分で作った CA を使っているときは `GORVIZ_CA_FINGERPRINT` に server が示した CA の SHA-256 を渡す。手元の CA の証明書なら `GORVIZ_CA_FILE`
- 401 が返ったら、トークンが無いか無効にされている。server の管理者に発行してもらう
