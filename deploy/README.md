# Kubernetes・ECS・Cloud Run での配置の例

docs/design.md の「agent とアプリ」(3.2.2) の置き方の例。どれも PoC で、実際のクラスタでは試していない。

- `kubernetes/server.yaml`
  - gorviz-server。クラスタの中に 1 つ置き、保存先は PersistentVolume (ローカルディスク) にする
- `kubernetes/daemonset.yaml`
  - daemon 型。ノードごとに gorviz-agent を 1 つ置き、annotation `gorviz/pprof-port` を付けた同じノードの Pod を観測する。ログはノードの `/var/log/pods` から読む
- `kubernetes/sidecar.yaml`
  - sidecar 型。アプリと同じ Pod に gorviz-agent を並べ、ログは共有ボリュームのファイルから読む
- `ecs/sidecar-task-definition.json`
  - sidecar 型 (Fargate)。ログは FireLens (Fluent Bit) から agent の tcp に送る
- `cloudrun/service.yaml`
  - sidecar 型 (Cloud Run)。ログは in-memory の共有ボリュームのファイルから読む
- ライブラリ型はアプリで `github.com/kakkky/gorviz` を import し、`gorviz.Run` で agent を埋め込む (README を参照)

## 共通

- agent は server へ外向きにつなぐので、アプリの Pod・タスクに外からの入口を開けなくてよい
- トークンは Secret に入れ、server には `GORVIZ_BOOTSTRAP_TOKEN`、agent には `GORVIZ_TOKEN` で渡す
- server が自分で作った CA を使うときは、server の起動ログに出る `ca_sha256` を agent の `GORVIZ_CA_FINGERPRINT` に渡す。手元の CA なら `GORVIZ_TLS_CERT_FILE`・`GORVIZ_TLS_KEY_FILE` (server) と `GORVIZ_CA_FILE` (agent)
- 保存先の容量の上限 `GORVIZ_MAX_DISK_BYTES` は、ボリュームの大きさより小さくする

## Cloud Run での注意

- CPU を常に割り当てる (`run.googleapis.com/cpu-throttling: "false"`)。既定ではリクエストを処理していない間 CPU がほぼ割り当てられず、agent が trace を取れず server へも送れない
- gorviz-server は Cloud Run に置けない。Cloud Run が外から受け付けるのは HTTP だけで、agent からの TCP (7071) を待ち受けられないため。server は GKE か GCE に置き、Cloud Run からは Direct VPC egress でつなぐ
- どのインスタンスも hostname が `localhost` になるので、インスタンスの名前が重なる。PoC では区別しない (今後、metadata server の instance ID を名前に使う)
