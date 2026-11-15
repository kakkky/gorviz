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
