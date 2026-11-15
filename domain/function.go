package domain

import "strings"

// IsRuntimeFunction は G が「何をしているか」の説明に使えない、スケジューラ内部の関数か。
// sync や net などの標準ライブラリは何で止まっているかを表すので残す。
func IsRuntimeFunction(fn string) bool {
	return strings.HasPrefix(fn, "runtime.") || strings.HasPrefix(fn, "internal/")
}

// CurrentFunction はスタック (上が先) のうち、スケジューラ内部を除いた一番上の関数を返す。
func CurrentFunction(stack []string) string {
	for _, fn := range stack {
		if !IsRuntimeFunction(fn) {
			return fn
		}
	}
	if len(stack) > 0 {
		return stack[0]
	}
	return ""
}

// ShortFunctionName は "net/http.(*conn).serve" を "http.(*conn).serve" のようにパッケージパスを落として返す。
func ShortFunctionName(fn string) string {
	if i := strings.LastIndex(fn, "/"); i >= 0 {
		return fn[i+1:]
	}
	return fn
}

// wrapperPrefixes は goroutine を代わりに起動するライブラリの関数。これらは入口の関数にも生成元にも使わず、利用者が渡した関数を使う
var wrapperPrefixes = []string{
	"github.com/kakkky/scope.",
	"golang.org/x/sync/errgroup.",
	"sync.(*WaitGroup).Go",
	// pprof.Do は label を付けて関数を呼ぶだけなので、label を付けた G の入口として飛ばす
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

// EntryFunction はスタック (上が先) から、go 文で起動された関数を返す。
// 一番下の関数が goroutine を代わりに起動するライブラリのものなら、それを飛ばして利用者が渡した関数を返す
func EntryFunction(stack []string) string {
	for i := len(stack) - 1; i >= 0; i-- {
		if !IsWrapperFunction(stack[i]) {
			return stack[i]
		}
	}
	if len(stack) > 0 {
		return stack[len(stack)-1]
	}
	return ""
}
