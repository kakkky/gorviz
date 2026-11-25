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
