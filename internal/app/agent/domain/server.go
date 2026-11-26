//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_server.go -package=mock
package domain

// ServerSender は server へ観測データと応答を送る
type ServerSender interface {
	SendHello(h Hello) error
	SendTrace(p []byte) error
	SendResponse(r Response) error
	SendGoroutineLeakProfile(seq uint64, text []byte) error
	SendCoverage(files []CoverageFile) error
	SendBye() error
}

// ServerConnection は server との接続を終える。終える手順は 1 度だけ流し、2 度目以降は 1 度目が終わるのを待たせる
type ServerConnection interface {
	BeginFinish() bool
	WaitFinished()
	Close()
}
