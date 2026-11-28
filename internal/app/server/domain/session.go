package domain

import "time"

type AppState string

const (
	AppBuilding    AppState = "building"
	AppBuildFailed AppState = "build-failed"
	AppStarting    AppState = "starting"
	AppRunning     AppState = "running"
	AppExited      AppState = "exited"
)

// AppRun はセッションで動かすアプリ。ExitedAt はセッションの時間軸の上の時刻。
// ProcessExited はアプリのプロセスが終わったこと。Exited は、そのあと trace を読み終えて終わりを記録したこと
type AppRun struct {
	Package          string
	Args             []string
	State            AppState
	BuildError       string
	StartError       string
	GoVersion        string
	Procs            int
	PID              int
	Exited           bool
	ExitCode         int
	ExitedAt         time.Duration
	TraceTruncated   bool
	ShutdownTimedOut bool
	ProcessExited    bool
}

type DiagnosisRange struct {
	From time.Duration
	To   time.Duration
}

// Session は gorviz run から gorviz stop までの 1 回。server のプロセス 1 つが、セッション 1 つにあたる
type Session struct {
	App        AppRun
	Build      AppBuild
	Diagnosing bool
	Diagnoses  []DiagnosisRange
	Observed   Observed
	Collected  Collected
}

// AppBuild は pprof を開く処理を差し込んでビルドしたアプリ
type AppBuild struct {
	WorkDir     string
	Binary      string
	ModulePath  string
	ModuleDir   string
	MainPackage string
	MainDir     string
}

// TraceChunkLength は、アプリの pprof から 1 回で取る trace の長さ。アプリが終わるとき、取りかけの区切りは届かないので短くする
const TraceChunkLength = 250 * time.Millisecond

// goroutineleak プロファイルを取る間隔。取るたびに GC が走るので、待ちの goroutine が前に取ったときより増えたときだけ、GoroutineLeakMinGap を空けて取る。
// ほかの待ちが同じだけ減って数が増えないリークを逃さないよう、GoroutineLeakMaxGap ごとには増えなくても取る。
// アプリが終わった時点のものは取れないので、shutdown の間は GoroutineLeakShutdownGap ごとに取り、最後に取れたものを終わった時点の代わりにする
const (
	GoroutineLeakMinGap      = 15 * time.Second
	GoroutineLeakMaxGap      = 60 * time.Second
	GoroutineLeakShutdownGap = time.Second
)

// Collected は最後に goroutineleak プロファイルを取った時刻と、そのときの待ちの goroutine の数
type Collected struct {
	Leaks       time.Time
	LeakWaiting int
}

// LeaksDue は、いま goroutineleak プロファイルを取るか。waiting は、いまの待ちの goroutine の数
func LeaksDue(c Collected, now time.Time, waiting int, shuttingDown bool) bool {
	since := now.Sub(c.Leaks)
	if shuttingDown {
		return since >= GoroutineLeakShutdownGap
	}
	return since >= GoroutineLeakMaxGap || (waiting > c.LeakWaiting && since >= GoroutineLeakMinGap)
}
