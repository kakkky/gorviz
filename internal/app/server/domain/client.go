//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_client.go -package=mock
package domain

import (
	"context"
	"time"
)

// AgentInstructor は agent に指示を送り、返事を待つ
type AgentInstructor interface {
	WaitConnected(ctx context.Context) error
	Connected() bool
	WriteTraceEvent(ctx context.Context, event string) error
	ResetCoverage(ctx context.Context) error
	ReadCoverage(ctx context.Context) ([]CoverageFile, error)
	RequestGoroutineLeaks(ctx context.Context) error
	RequestThreadCount(ctx context.Context) error
	Finish(ctx context.Context) error
	WaitDisconnected(ctx context.Context) error
}

// BuildAppInput の Dir は gorviz run を実行したディレクトリ
type BuildAppInput struct {
	Dir     string
	Package string
	Env     []string
}

// AppBuilder は agent を差し込んで、coverage を取れるようにアプリをビルドする
type AppBuilder interface {
	AgentFiles() (agent []SourceFile, protocol []SourceFile)
	LoadMainPackage(ctx context.Context, in BuildAppInput) (MainPackage, AppBuild, error)
	BuildApp(ctx context.Context, in BuildAppInput, build AppBuild, overlay Overlay) error
}

// AppProcess は子プロセスとして起動したアプリ。Wait はアプリが終わるまで待ち、終了コードを返す
type AppProcess interface {
	Terminate() error
	Kill() error
	Wait() int
}

// AppStarter はビルドしたアプリを子プロセスとして起動する
type AppStarter interface {
	StartApp(ctx context.Context, build AppBuild, args []string, env []string) (AppProcess, error)
}

// LoadClient は 1 つの VU。cookie を VU ごとに持つ
type LoadClient interface {
	Send(ctx context.Context, r Request) Response
}

// AppRequester はアプリへ負荷のリクエストを送る VU を作る
type AppRequester interface {
	NewLoadClient() LoadClient
}

// Toolchain は Go のツールチェーンでアプリの coverage を読み、ソースを読む
type Toolchain interface {
	CoverageProfile(ctx context.Context, files []CoverageFile) ([]byte, error)
	ReadAppSources(build AppBuild, files []string) map[string][]byte
}

// Clock は今の時刻を返す。待つ時間の判断に使う
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}
