package observation

import "time"

type LeakCheckStatus string

const (
	LeakCheckDone        LeakCheckStatus = "DONE"
	LeakCheckUnsupported LeakCheckStatus = "UNSUPPORTED"
	LeakCheckDisabled    LeakCheckStatus = "DISABLED"
)

type GoroutineLeakCheck struct {
	At     time.Time
	Status LeakCheckStatus
	Leaks  []LeakedStack
}

type LeakedStack struct {
	Stack []Frame
	Count int
}
