package workdir

import (
	"os"
	"path/filepath"
)

// WorkDir はセッションの間だけ使う一時ディレクトリ (アプリが pprof を開く socket・ビルドしたアプリ)。止めるときに消す
type WorkDir struct {
	path string
}

func NewWorkDir() (*WorkDir, func(), error) {
	p, err := os.MkdirTemp("", "gorviz-")
	if err != nil {
		return nil, nil, err
	}
	return &WorkDir{path: p}, func() { _ = os.RemoveAll(p) }, nil
}

func (w *WorkDir) Path() string { return w.path }

func (w *WorkDir) SocketPath() string { return filepath.Join(w.path, "pprof.sock") }
