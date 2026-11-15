# Kubernetes・ECS・Cloud Run での設定例

docs/design.md の「agent の置き方」の例。どれも PoC で、実際のクラスタでは試していない。

- `kubernetes/server.yaml`: gorviz-server (GUI / API は 7070、agent の口は 7071)。クラスタの中に 1 つ置き、外からは port-forward などで見る
- `kubernetes/daemonset.yaml`: daemon 型。ノードごとに gorviz-agent を 1 つ置き、同じノードの Pod の pprof を観測する
- `kubernetes/sidecar.yaml`: sidecar 型。アプリと同じ Pod に gorviz-agent を並べる
- `ecs/sidecar-task-definition.json`: sidecar 型 (Fargate)。アプリと同じタスクに gorviz-agent を並べる
- `cloudrun/service.yaml`: sidecar 型 (Cloud Run)。アプリと同じサービスに gorviz-agent を並べる。GKE は `kubernetes/` をそのまま使う
- ライブラリ型はアプリで `github.com/kakkky/gorviz` を import し、`gorviz.Start` で agent を埋め込む (example/library を参照)

agent は server へ外向きにつなぐので、アプリの Pod / タスクに外からの入口を開ける必要はない。
観測するかどうかは対象側で明示的に許可する: daemon 型では、agent に渡す `-target` を、許可した Pod (annotation `gorviz.kakkky.dev/observe: "true"`) の pprof に限る。
annotation から対象を自動で見つける仕組みは未実装で、今は `-target` を手で並べる。

Cloud Run での注意:

- CPU を常に割り当てる (`run.googleapis.com/cpu-throttling: "false"`、インスタンス単位の課金)。既定ではリクエストを処理していない間 CPU がほぼ割り当てられず、agent が trace を取れず server へも送れない
- gorviz-server は Cloud Run に置けない。Cloud Run が外から受け付けるのは HTTP (WebSocket・gRPC を含む) だけで、agent からの TCP (7071) を待ち受けられないため。server は GKE か GCE に置き、Cloud Run からは Direct VPC egress でつなぐ
- サイドカーでは、外からのリクエストを受けるコンテナ (アプリ) のポートを明示する必要がある
