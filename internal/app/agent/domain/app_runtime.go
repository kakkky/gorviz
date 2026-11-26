//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_app_runtime.go -package=mock
package domain

// AppRuntimeProcess はアプリのプロセスの素性を読む
type AppRuntimeProcess interface {
	GoVersion() string
	Procs() int
	PID() int
}

// AppRuntimeTrace はアプリの execution trace を始め、流れてくる中身を読み、止める
type AppRuntimeTrace interface {
	StartTrace() error
	// ReadTrace は trace を止めて流れ切ったら io.EOF を返す
	ReadTrace(p []byte) (int, error)
	StopTrace()
	WaitTraceDrained()
	WriteTraceEvent(event string)
}

// AppRuntimeCoverage はアプリの coverage のカウンタを 0 にし、書き出す
type AppRuntimeCoverage interface {
	ResetCoverage() error
	ReadCoverage() ([]CoverageFile, error)
}

// AppRuntimeGoroutines はアプリの goroutine とスレッドについて runtime に問い合わせる
type AppRuntimeGoroutines interface {
	NextLeakCheckSeq() uint64
	ReadGoroutineLeakProfile() ([]byte, error)
	ThreadCount() int
}
