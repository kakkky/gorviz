package model

import "time"

// GoroutineDump は agent が取った goroutine dump 1 回分。LeakCheck はその dump で runtime のリーク判定を走らせたか
type GoroutineDump struct {
	At         time.Time
	Goroutines []Goroutine
	LeakCheck  LeakCheckStatus
}

type LeakCheckStatus string

const (
	LeakCheckDone        LeakCheckStatus = "done"
	LeakCheckSkipped     LeakCheckStatus = "skipped"
	LeakCheckUnsupported LeakCheckStatus = "unsupported"
	LeakCheckDisabled    LeakCheckStatus = "disabled"
)
