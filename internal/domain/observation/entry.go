package observation

import (
	"strings"

	"github.com/kakkky/gorviz/internal/pkg/funcname"
)

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
		if !funcname.IsRuntime(f.Function) {
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
		if !funcname.IsStdlib(f.Function) {
			return f
		}
	}
	return CurrentFrame(stack)
}
