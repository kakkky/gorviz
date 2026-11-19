// Package logmark は、ログ 1 行とそれを書いた goroutine を結ぶ仕組み。直下の slog ハンドラと logger/ のアダプタが共有する。
//   - ログを書く goroutine の上で番号を trace に書き、同じ番号をログ行の属性に足す
//   - アプリに埋め込んだ agent があれば、出力されたログ行をそのまま渡す
package logmark

import (
	"bytes"
	"context"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"runtime/trace"
	"strconv"
	"sync/atomic"
)

const Key = model.LogSeqAttrKey

var (
	seq     atomic.Uint64
	forward atomic.Pointer[func([]byte)]
)

// Mark は trace が有効なとき (gorviz が trace を取っている間) だけ番号を振る。
// trace.Log は呼んだ goroutine の上でイベントになるので、ログを書く goroutine の上で呼ぶ
func Mark(ctx context.Context) (uint64, bool) {
	if !trace.IsEnabled() {
		return 0, false
	}
	n := seq.Add(1)
	trace.Log(ctx, model.LogMarkCategory, strconv.FormatUint(n, 10))
	return n, true
}

// SetForward は埋め込んだ agent がログ行を受け取る関数を設定する。nil で外す
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
