package domain

import (
	"regexp"
	"strconv"
	"strings"
)

type Frame struct {
	Function string
	File     string
	Line     int
}

func (f Frame) Location() string {
	if f.File == "" {
		return ""
	}
	return f.File + ":" + strconv.Itoa(f.Line)
}

// AgentPackage は agent をアプリに差し込むときのパッケージの名前。作られたときのスタックにこれがある goroutine は agent のもの
const AgentPackage = "gorvizagent"

func IsAgentFunction(fn string) bool {
	return strings.Contains(fn, "/"+AgentPackage+".") || strings.Contains(fn, "/"+AgentPackage+"/")
}

// IsRuntimeFunction はスケジューラの内部で、何をしているかの説明に使えない関数か。sync や net は何で止まっているかを表すので含めない
func IsRuntimeFunction(fn string) bool {
	return strings.HasPrefix(fn, "runtime.") || strings.HasPrefix(fn, "internal/")
}

// IsStdlibFunction はパッケージパスの先頭にドメインがないものを標準ライブラリとみなす。main は除く
func IsStdlibFunction(fn string) bool {
	first, _, _ := strings.Cut(fn, "/")
	if !strings.Contains(fn, "/") {
		first, _, _ = strings.Cut(fn, ".")
	}
	return first != "main" && !strings.Contains(first, ".")
}

// wrapperPrefixes は goroutine を代わりに起動するライブラリの関数。入口にも生成元にも使わず、利用者が渡した関数を使う
var wrapperPrefixes = []string{
	"github.com/kakkky/scope.",
	"golang.org/x/sync/errgroup.",
	"sync.(*WaitGroup).Go",
	"runtime/pprof.Do",
}

func IsWrapperFunction(fn string) bool {
	for _, p := range wrapperPrefixes {
		if strings.HasPrefix(fn, p) {
			return true
		}
	}
	return false
}

// EntryFunction はスタック (上が先) の一番下から、ライブラリの起動の関数を飛ばした関数
func EntryFunction(stack []Frame) string {
	for i := len(stack) - 1; i >= 0; i-- {
		if !IsWrapperFunction(stack[i].Function) {
			return stack[i].Function
		}
	}
	if len(stack) > 0 {
		return stack[len(stack)-1].Function
	}
	return ""
}

// CurrentFrame はスケジューラの内部を除いた一番上のフレーム
func CurrentFrame(stack []Frame) Frame {
	for _, f := range stack {
		if !IsRuntimeFunction(f.Function) {
			return f
		}
	}
	if len(stack) > 0 {
		return stack[0]
	}
	return Frame{}
}

// UserFrame は利用者のコード (標準ライブラリと runtime を除く) の一番上のフレーム。なければ CurrentFrame
func UserFrame(stack []Frame) Frame {
	for _, f := range stack {
		if !IsStdlibFunction(f.Function) && !IsWrapperFunction(f.Function) {
			return f
		}
	}
	return CurrentFrame(stack)
}

var (
	typeParams = regexp.MustCompile(`\[[^\[\]]*\]`)
	closure    = regexp.MustCompile(`(\.(func|gowrap|deferwrap)\d+|\.\d+)+$`)
)

// IssueFunction は Issue を数える単位の関数名。無名関数は外側の関数に、型パラメータは "[...]" にならす
func IssueFunction(fn string) string {
	return closure.ReplaceAllString(typeParams.ReplaceAllString(fn, "[...]"), "")
}

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
