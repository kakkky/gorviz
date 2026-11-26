package server

// HTTP API のエンドポイントのパス。メソッドは待ち受ける側と呼ぶ側がそれぞれ付ける
const (
	StatusEndpoint      = "/api/status"
	StartResultEndpoint = "/api/start-result"
	DiagnoseEndpoint    = "/api/diagnose"
	IssuesEndpoint      = "/api/issues"
	IssueEndpoint       = "/api/issues/{id}"
	GoroutineEndpoint   = "/api/goroutines/{id}"
	StopEndpoint        = "/api/stop"
)

// HeaderExitCode は、cli がそのまま使う終了コード。途中の結果を流す応答では trailer で返す
const HeaderExitCode = "Gorviz-Exit-Code"

const (
	MediaTOON = "application/toon"
	MediaJSON = "application/json"
)
