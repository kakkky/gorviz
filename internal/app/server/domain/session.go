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

// 外から書き出させる観測データの間隔
const (
	ThreadCountEvery   = 2 * time.Second
	GoroutineLeakEvery = 15 * time.Second
)

// Collected は agent に最後に書き出させた時刻
type Collected struct {
	Threads time.Time
	Leaks   time.Time
}

type DueCollection struct {
	Threads bool
	Leaks   bool
}

func DueCollections(c Collected, now time.Time) DueCollection {
	return DueCollection{Threads: now.Sub(c.Threads) >= ThreadCountEvery, Leaks: now.Sub(c.Leaks) >= GoroutineLeakEvery}
}

func MarkCollected(c Collected, due DueCollection, now time.Time) Collected {
	if due.Threads {
		c.Threads = now
	}
	if due.Leaks {
		c.Leaks = now
	}
	return c
}
