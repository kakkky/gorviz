package domain

import "github.com/kakkky/gorviz/internal/protocol/agentsocket"

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

const AppExitEvent = agentsocket.EventAppExit

func LeakCheckEvent(seq uint64) string {
	return agentsocket.LeakCheckEvent(seq)
}

func ThreadCountEvent(n int) string {
	return agentsocket.ThreadCountEvent(n)
}
