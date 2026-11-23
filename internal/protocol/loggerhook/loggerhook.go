// Package loggerhook はプロセスの中の契約。ロガーアダプタがログ行と trace に書く番号の決まりと、ライブラリ型で埋め込んだ agent へ行を渡す口
package loggerhook

import (
	"bytes"
	"context"
	"runtime/trace"
	"strconv"
	"sync/atomic"
)

const (
	// SeqKey はロガーアダプタがログ行に足すフィールドのキー
	SeqKey = "gorviz_seq"
	// Category はロガーアダプタが trace.Log に渡すカテゴリ
	Category = "gorviz.log"
)

var (
	seq     atomic.Uint64
	forward atomic.Pointer[func([]byte)]
)

// Mark は trace を取っている間だけ番号を振る。trace.Log は呼んだ goroutine のイベントになるので、ログを書く goroutine の上で呼ぶ
func Mark(ctx context.Context) (uint64, bool) {
	if !trace.IsEnabled() {
		return 0, false
	}
	n := seq.Add(1)
	trace.Log(ctx, Category, strconv.FormatUint(n, 10))
	return n, true
}

// SetForward は埋め込んだ agent が行を受け取る関数を設定する。nil で外す。f はアプリの呼び出しの中で呼ばれるので、ブロックしてはいけない
func SetForward(f func([]byte)) {
	if f == nil {
		forward.Store(nil)
		return
	}
	forward.Store(&f)
}

func Forwarding() bool {
	return forward.Load() != nil
}

// Forward はロガーが書いた後にバッファを使い回すことがあるので、複製して渡す
func Forward(line []byte) {
	if f := forward.Load(); f != nil {
		(*f)(bytes.Clone(line))
	}
}
