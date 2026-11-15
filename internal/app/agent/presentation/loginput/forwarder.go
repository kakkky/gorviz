package loginput

import (
	"context"
	"io"
)

// Forwarder は埋め込んだ agent が、アプリのロガーから渡された行 (logmark.Forward) を受け取る口
type Forwarder struct {
	lines chan []byte
}

func NewForwarder(size int) *Forwarder {
	return &Forwarder{lines: make(chan []byte, size)}
}

// Enqueue は待たない。agent が読み切れないほどログが多いときは、アプリを止めないよう捨てる
func (f *Forwarder) Enqueue(line []byte) {
	select {
	case f.lines <- line:
	default:
	}
}

// Reader は受け取った行を順に読める Reader を返す。ctx が終わると EOF になる
func (f *Forwarder) Reader(ctx context.Context) io.ReadCloser {
	r, w := io.Pipe()
	go func() {
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case line := <-f.lines:
				if _, err := w.Write(line); err != nil {
					return
				}
			}
		}
	}()
	return r
}
