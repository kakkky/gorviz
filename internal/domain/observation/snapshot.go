package observation

import "time"

type GoroutineState string

const (
	GoroutineRunning  GoroutineState = "RUNNING"
	GoroutineRunnable GoroutineState = "RUNNABLE"
	GoroutineWaiting  GoroutineState = "WAITING"
	GoroutineSyscall  GoroutineState = "SYSCALL"
)

// GoroutineSnapshot は trace のチャンクの終わりの時点で生きている全 goroutine
type GoroutineSnapshot struct {
	At         time.Time
	Goroutines []Goroutine
}

// Goroutine の WaitReasonInferred はスタックから推定した待機理由か、WaitingLowerBound は agent が見始める前から待っていて下限しか分からないか、
// CreatedByInferred は生成元が分からず入口で代用したか
type Goroutine struct {
	ID                 GoroutineID
	State              GoroutineState
	WaitReason         string
	WaitReasonInferred bool
	Waiting            time.Duration
	WaitingLowerBound  bool
	Entry              string
	Current            Frame
	CreatedBy          Frame
	CreatedByInferred  bool
	Stack              []Frame
	WokenBy            GoroutineID
	WokenByAt          Frame
}
