package domain

import "strings"

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
	"sync.runtime_SemacquireMutex",
	"sync.runtime_SemacquireRWMutex",
	"internal/sync.(*Mutex).Lock",
	"internal/sync.runtime_SemacquireMutex",
}

// IsLockStack は sync.Mutex・sync.RWMutex の Lock での待ちか。待機理由の「sync」には sync.(*WaitGroup).Wait などの待ちも入るので、スタックで見分ける
func IsLockStack(stack []Frame) bool {
	for _, f := range stack {
		for _, p := range lockFunctions {
			if strings.HasPrefix(f.Function, p) {
				return true
			}
		}
	}
	return false
}

func isCgoStack(stack []Frame) bool {
	for _, f := range stack {
		if strings.HasPrefix(f.Function, "runtime.cgocall") {
			return true
		}
	}
	return false
}

// CauseOfWait は trace の待機理由と、待ちに入ったときのスタックから主因を決める
func CauseOfWait(reason string, stack []Frame) MainCause {
	r := strings.ToLower(reason)
	switch {
	case isCgoStack(stack):
		return CauseCgo
	case r == "network" || strings.Contains(r, "io wait") || r == "system goroutine wait" && hasFunction(stack, "internal/poll."):
		return CauseIOWait
	case strings.Contains(r, "sleep") || strings.Contains(r, "timer"):
		return CauseSleep
	case strings.Contains(r, "gc") || strings.Contains(r, "garbage"):
		return CauseGC
	case IsLockStack(stack):
		return CauseLockWait
	}
	return CauseSyncWait
}

// InferWaitReason は agent より前に待ちに入っていて待機理由が分からない goroutine の待機理由を、スタックの上の方の runtime の関数から推定する
func InferWaitReason(stack []Frame) string {
	for _, f := range stack {
		switch {
		case strings.HasPrefix(f.Function, "runtime.chanrecv"):
			return "chan receive"
		case strings.HasPrefix(f.Function, "runtime.chansend"):
			return "chan send"
		case strings.HasPrefix(f.Function, "runtime.selectgo"):
			return "select"
		case strings.HasPrefix(f.Function, "runtime.block"):
			return "select (no cases)"
		case strings.HasPrefix(f.Function, "time.Sleep"):
			return "sleep"
		case strings.HasPrefix(f.Function, "internal/poll.") || strings.HasPrefix(f.Function, "runtime.netpollblock"):
			return "network"
		case strings.HasPrefix(f.Function, "sync."), strings.HasPrefix(f.Function, "internal/sync."):
			return "sync"
		}
	}
	return "unknown"
}

func hasFunction(stack []Frame, prefix string) bool {
	for _, f := range stack {
		if strings.HasPrefix(f.Function, prefix) {
			return true
		}
	}
	return false
}
