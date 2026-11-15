package domain

import "time"

// SchedTraceReport は GODEBUG=schedtrace,scheddetail で runtime 自身が出す、ある瞬間のスケジューラの状態。
// trace からの推測と違い run queue の長さが正確だが、対象をその環境変数つきで起動したときにしか取れない。
type SchedTraceReport struct {
	Uptime          time.Duration
	GOMAXPROCS      int
	IdleProcs       int
	Threads         int
	SpinningThreads int
	IdleThreads     int
	GlobalRunQueue  int
	Procs           []SchedTraceProc
	// GoroutineStates は G の状態ごとの数。scheddetail の G 行の status から数える (dead を含む)
	GoroutineStates map[string]int
}

// SchedTraceProc の Thread は runtime の M の番号で、trace 由来の M の番号 (観測窓ごとに振り直したもの) とは対応しない。
type SchedTraceProc struct {
	Proc      ProcID
	Status    string
	Thread    int
	HasThread bool
	RunQueue  int
	GFree     int
}

type SchedTraceSource interface {
	Latest() (SchedTraceReport, bool)
}
