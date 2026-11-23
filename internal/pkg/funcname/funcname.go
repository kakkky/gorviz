// Package funcname は、runtime がスタックに書く Go の関数名を読む
package funcname

import (
	"regexp"
	"strings"
)

// IsRuntime は scheduler の内部で、何をしているかの説明に使えない関数か。sync や net は何で止まっているかを表すので含めない
func IsRuntime(fn string) bool {
	return strings.HasPrefix(fn, "runtime.") || strings.HasPrefix(fn, "internal/")
}

// IsStdlib はパッケージパスの先頭にドメインがないものを標準ライブラリとみなす。main は除く
func IsStdlib(fn string) bool {
	first, _, _ := strings.Cut(fn, "/")
	if !strings.Contains(fn, "/") {
		first, _, _ = strings.Cut(fn, ".")
	}
	return first != "main" && !strings.Contains(first, ".")
}

// Short は "net/http.(*conn).serve" を "http.(*conn).serve" にする
func Short(fn string) string {
	if i := strings.LastIndex(fn, "/"); i >= 0 {
		return fn[i+1:]
	}
	return fn
}

var (
	typeParams = regexp.MustCompile(`\[[^\[\]]*\]`)
	closure    = regexp.MustCompile(`(\.(func|gowrap|deferwrap)\d+|\.\d+)+$`)
)

// Outer は無名関数 (handle.func1・handle.func1.2・handle.gowrap1) を外側の関数にする
func Outer(fn string) string {
	return closure.ReplaceAllString(fn, "")
}

// FoldTypeParams は型パラメータの部分 ("F[go.shape.int]" など) を "[...]" にならす
func FoldTypeParams(fn string) string {
	return typeParams.ReplaceAllString(fn, "[...]")
}
