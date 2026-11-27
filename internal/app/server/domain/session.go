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

// AppRun はセッションで動かすアプリ。ExitedAt はセッションの時間軸の上の時刻
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
}

type DiagnosisRange struct {
	From time.Duration
	To   time.Duration
}

// Session は gorviz run から gorviz stop までの 1 回。server のプロセス 1 つが、セッション 1 つにあたる
type Session struct {
	App          AppRun
	Build        AppBuild
	Diagnosing   bool
	Diagnoses    []DiagnosisRange
	Observed     Observed
	LastCoverage []CoverageFile
	Collected    Collected
}

// AppBuild は agent を差し込んでビルドしたアプリ
type AppBuild struct {
	WorkDir       string
	Binary        string
	CoverPackages []string
	ModulePath    string
	ModuleDir     string
	MainPackage   string
	MainDir       string
}

type CoverageFile struct {
	Name string
	Data []byte
}

// agent に書き出させる観測データの間隔。goroutineleak プロファイルは取るたびに GC が走るので、
// 待ちの goroutine が前に取ったときより増えたときだけ、GoroutineLeakMinGap を空けて取る。
// ほかの待ちが同じだけ減って数が増えないリークを逃さないよう、GoroutineLeakMaxGap ごとには増えなくても取る
const (
	ThreadCountEvery    = 2 * time.Second
	GoroutineLeakMinGap = 15 * time.Second
	GoroutineLeakMaxGap = 60 * time.Second
)

// Collected は agent に最後に書き出させた時刻と、goroutineleak プロファイルを取ったときの待ちの goroutine の数
type Collected struct {
	Threads     time.Time
	Leaks       time.Time
	LeakWaiting int
}

type DueCollection struct {
	Threads bool
	Leaks   bool
}

// DueCollections の waiting は、いまの待ちの goroutine の数
func DueCollections(c Collected, now time.Time, waiting int) DueCollection {
	sinceLeaks := now.Sub(c.Leaks)
	return DueCollection{
		Threads: now.Sub(c.Threads) >= ThreadCountEvery,
		Leaks:   sinceLeaks >= GoroutineLeakMaxGap || (waiting > c.LeakWaiting && sinceLeaks >= GoroutineLeakMinGap),
	}
}

func MarkCollected(c Collected, due DueCollection, now time.Time, waiting int) Collected {
	if due.Threads {
		c.Threads = now
	}
	if due.Leaks {
		c.Leaks, c.LeakWaiting = now, waiting
	}
	return c
}
