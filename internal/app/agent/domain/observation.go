package domain

import "github.com/kakkky/gorviz/internal/protocol/agent"

type Hello struct {
	GoVersion string
	Procs     int
	PID       int
}

type CoverageFile struct {
	Name string
	Data []byte
}

type Reply struct {
	ID       uint64
	Error    string
	Coverage []CoverageFile
}

const AppExitEvent = agent.EventAppExit

func LeakCheckEvent(seq uint64) string {
	return agent.LeakCheckEvent(seq)
}

func ThreadCountEvent(n int) string {
	return agent.ThreadCountEvent(n)
}
