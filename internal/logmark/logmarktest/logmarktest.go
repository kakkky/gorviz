// Package logmarktest は、ログのアダプタのテストで trace の印と agent に渡ったログ行を集める
package logmarktest

import (
	"bytes"
	"errors"
	"io"
	"runtime/trace"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	xtrace "golang.org/x/exp/trace"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/logmark"
)

type Result struct {
	// Marks は trace に書かれた番号
	Marks []uint64
	// Forwarded は埋め込んだ agent に渡ったログ行
	Forwarded [][]byte
}

// Capture は trace と agent への受け渡しを有効にして f を呼ぶ。プロセスで 1 つの trace を使うので、並列に呼ばない
func Capture(t *testing.T, f func()) Result {
	t.Helper()
	var (
		mu  sync.Mutex
		res Result
	)
	logmark.SetForward(func(line []byte) {
		mu.Lock()
		defer mu.Unlock()
		res.Forwarded = append(res.Forwarded, line)
	})
	defer logmark.SetForward(nil)

	var buf bytes.Buffer
	require.NoError(t, trace.Start(&buf))
	f()
	trace.Stop()

	r, err := xtrace.NewReader(&buf)
	require.NoError(t, err)
	for {
		ev, err := r.ReadEvent()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if ev.Kind() == xtrace.EventLog && ev.Log().Category == model.LogMarkCategory {
			n, err := strconv.ParseUint(ev.Log().Message, 10, 64)
			require.NoError(t, err)
			res.Marks = append(res.Marks, n)
		}
	}
	return res
}
