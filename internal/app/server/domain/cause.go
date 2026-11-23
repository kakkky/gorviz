package domain

import (
	"strings"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// MainCause は goroutine が止まっていた (走っていた) 理由の分類
type MainCause string

const (
	CauseRunning  MainCause = "RUNNING"
	CauseRunnable MainCause = "RUNNABLE"
	CauseLockWait MainCause = "LOCK_WAIT"
	CauseSyncWait MainCause = "SYNC_WAIT"
	CauseIOWait   MainCause = "IO_WAIT"
	CauseCgo      MainCause = "CGO"
	CauseSleep    MainCause = "SLEEP"
	CauseGC       MainCause = "GC"
)

// MainCauses は同じ長さのときに選ぶ順でもある
var MainCauses = []MainCause{CauseRunning, CauseRunnable, CauseLockWait, CauseSyncWait, CauseIOWait, CauseCgo, CauseSleep, CauseGC}

var lockFunctions = []string{
	"sync.(*Mutex).Lock",
	"sync.(*RWMutex).Lock",
	"sync.(*RWMutex).RLock",
	// 待ちに入ったときに一番上に残る sync の関数。sync.(*Mutex).Lock からしか呼ばれない
	"sync.runtime_SemacquireMutex",
	"sync.runtime_SemacquireRWMutex",
}

// IsLockFunction は sync.Mutex・sync.RWMutex の Lock での待ちか。sync.(*WaitGroup).Wait などの「sync」の待ちを除くため
func IsLockFunction(fn string) bool {
	for _, p := range lockFunctions {
		if strings.HasPrefix(fn, p) {
			return true
		}
	}
	return false
}

func IsLockStack(stack []observation.Frame) bool {
	for _, f := range stack {
		if IsLockFunction(f.Function) {
			return true
		}
	}
	return false
}

// CauseOfWait は待機理由と、待ちに入ったときに止まった関数から主因を決める。
// trace の理由 ("network"・"sync" など) と、スタックから推定した理由 ("IO wait"・"semacquire" など) のどちらも受け付ける
func CauseOfWait(reason string, lock bool) MainCause {
	r := strings.ToLower(reason)
	switch {
	case r == "network" || strings.Contains(r, "io wait"):
		return CauseIOWait
	case strings.Contains(r, "sleep") || strings.Contains(r, "timer"):
		return CauseSleep
	case strings.Contains(r, "gc"):
		return CauseGC
	case lock:
		return CauseLockWait
	}
	return CauseSyncWait
}

// CauseOfPlace は状態の変化 1 つが移った先の主因。dead は主因を持たない
func CauseOfPlace(p observation.Place, function string) (MainCause, bool) {
	switch p.Kind {
	case observation.PlaceRunning:
		return CauseRunning, true
	case observation.PlaceLocalQueue, observation.PlaceGlobalQueue:
		return CauseRunnable, true
	case observation.PlaceSyscall:
		if p.Cgo {
			return CauseCgo, true
		}
		return CauseIOWait, true
	case observation.PlaceWaiting:
		if p.Cgo {
			return CauseCgo, true
		}
		return CauseOfWait(p.Reason, IsLockFunction(function)), true
	}
	return "", false
}

func hasCgoCall(stack []observation.Frame) bool {
	for _, f := range stack {
		if strings.HasPrefix(f.Function, "runtime.cgocall") {
			return true
		}
	}
	return false
}

// CauseOfSnapshot はスナップショットの 1 時点の状態から主因を決める
func CauseOfSnapshot(g observation.Goroutine) MainCause {
	switch g.State {
	case observation.GoroutineRunning:
		return CauseRunning
	case observation.GoroutineRunnable:
		return CauseRunnable
	case observation.GoroutineSyscall:
		if hasCgoCall(g.Stack) {
			return CauseCgo
		}
		return CauseIOWait
	}
	return CauseOfWait(g.WaitReason, IsLockStack(g.Stack))
}
