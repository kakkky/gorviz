//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_runtime.go -package=mock
package domain

// AppProcess はアプリのプロセスの素性を読む
type AppProcess interface {
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

// AppCoverage はアプリの coverage のカウンタを 0 にし、書き出す
type AppCoverage interface {
	ResetCoverage() error
	ReadCoverage() ([]CoverageFile, error)
}

// AppGoroutines はアプリの goroutine とスレッドについて runtime に問い合わせる
type AppGoroutines interface {
	NextLeakCheckSeq() uint64
	ReadGoroutineLeakProfile() ([]byte, error)
	ThreadCount() int
}
