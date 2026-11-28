//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_client.go -package=mock
package domain

import (
	"context"
	"time"
)

// BuildAppInput の Dir は gorviz run を実行したディレクトリ
type BuildAppInput struct {
	Dir     string
	Package string
	Env     []string
}

// AppBuilder は pprof を開く処理を差し込んで、アプリをビルドする。GoVersion はビルドしたアプリの Go のバージョン
type AppBuilder interface {
	LoadMainPackage(ctx context.Context, in BuildAppInput) (MainPackage, AppBuild, error)
	BuildApp(ctx context.Context, in BuildAppInput, build AppBuild, overlay Overlay) error
	GoVersion(build AppBuild) (string, error)
}

// AppProcess は子プロセスとして起動したアプリ。Wait はアプリが終わるまで待ち、終了コードを返す
type AppProcess interface {
	PID() int
	Terminate() error
	Kill() error
	Wait() int
}

// AppStarter はビルドしたアプリを子プロセスとして起動する
type AppStarter interface {
	StartApp(ctx context.Context, build AppBuild, args []string, env []string) (AppProcess, error)
}

// AppProfiler は、アプリが開いた pprof から trace とプロファイルを取る。アプリが動いていなければ取れない
type AppProfiler interface {
	FetchTrace(ctx context.Context, length time.Duration) ([]byte, error)
	FetchThreadCreateProfile(ctx context.Context) ([]byte, error)
	FetchGoroutineLeakProfile(ctx context.Context) ([]byte, error)
}

// LoadClient は 1 つの VU。cookie を VU ごとに持つ
type LoadClient interface {
	Send(ctx context.Context, r Request) Response
}

// AppRequester はアプリへ負荷のリクエストを送る VU を作る
type AppRequester interface {
	NewLoadClient() LoadClient
}

// Clock は今の時刻を返す。待つ時間の判断に使う
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}
