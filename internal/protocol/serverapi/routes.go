package serverapi

// HTTP API のルート
const (
	PathStatus      = "/api/status"
	PathStartResult = "/api/start-result"
	PathDiagnose    = "/api/diagnose"
	PathIssues      = "/api/issues"
	PathIssue       = "/api/issues/{id}"
	PathGoroutine   = "/api/goroutines/{id}"
	PathStop        = "/api/stop"
)

// HeaderExitCode は、cli がそのまま使う終了コード。途中の結果を流す応答では trailer で返す
const HeaderExitCode = "Gorviz-Exit-Code"

const (
	MediaTOON = "application/toon"
	MediaJSON = "application/json"
)
