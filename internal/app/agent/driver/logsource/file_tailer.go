package logsource

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/logs"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// FileTailer はワイルドカードに当たるファイルを tail する。間隔ごとに当たるファイルを見直し、ローテーションに追従する。
// 起動したときにあったファイルは末尾から、後から現れたファイルは先頭から読む
type FileTailer struct {
	receiver *logs.Receiver
	globs    []string
	interval time.Duration
	maxBytes int
	files    map[string]*tailedFile
}

type tailedFile struct {
	f      *os.File
	r      *bufio.Reader
	info   os.FileInfo
	offset int64
	line   []byte
	joiner lineJoiner
}

func NewFileTailer(cfg *config.Config, receiver *logs.Receiver) (*FileTailer, error) {
	globs := cfg.LogFiles
	if cfg.Mode == config.ModeDaemon {
		globs = append(globs, filepath.Join(cfg.PodLogDir, "*", "*", "*.log"))
	}
	for _, g := range globs {
		if _, err := filepath.Match(g, ""); err != nil {
			return nil, err
		}
	}
	return &FileTailer{receiver: receiver, globs: globs, interval: cfg.LogPollInterval, maxBytes: cfg.LogMaxLineBytes, files: map[string]*tailedFile{}}, nil
}

func (t *FileTailer) Run(ctx context.Context) error {
	defer t.closeAll()
	if len(t.globs) == 0 {
		<-ctx.Done()
		return nil
	}
	t.scan(ctx, true)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(t.interval):
		}
		t.scan(ctx, false)
		for path, f := range t.files {
			t.follow(ctx, path, f)
		}
	}
}

func (t *FileTailer) scan(ctx context.Context, atStart bool) {
	seen := map[string]bool{}
	for _, g := range t.globs {
		paths, _ := filepath.Glob(g)
		for _, p := range paths {
			seen[p] = true
			if _, ok := t.files[p]; ok {
				continue
			}
			f, err := t.open(p, atStart)
			if err != nil {
				slog.WarnContext(ctx, "open log file", slog.String("path", p), slog.Any("err", err))
				continue
			}
			t.files[p] = f
		}
	}
	for p, f := range t.files {
		if !seen[p] {
			t.drain(ctx, p, f)
			f.f.Close()
			delete(t.files, p)
		}
	}
}

func (t *FileTailer) open(path string, atEnd bool) (*tailedFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	tf := &tailedFile{f: f, info: info, joiner: lineJoiner{maxBytes: t.maxBytes}}
	if atEnd {
		if tf.offset, err = f.Seek(0, io.SeekEnd); err != nil {
			f.Close()
			return nil, err
		}
	}
	tf.r = bufio.NewReader(f)
	return tf, nil
}

// follow は書き足された分を読む。同じパスに別のファイルができていれば (ローテーション)、前のファイルを読み切ってから新しいファイルを先頭から読む。
// 切り詰められていれば (copytruncate) 先頭から読み直す
func (t *FileTailer) follow(ctx context.Context, path string, f *tailedFile) {
	t.drain(ctx, path, f)
	cur, err := os.Stat(path)
	if err != nil {
		return
	}
	switch {
	case !os.SameFile(cur, f.info):
		f.f.Close()
		nf, err := t.open(path, false)
		if err != nil {
			delete(t.files, path)
			return
		}
		t.files[path] = nf
		t.drain(ctx, path, nf)
	case cur.Size() < f.offset:
		if _, err := f.f.Seek(0, io.SeekStart); err == nil {
			f.offset, f.line = 0, nil
			f.r.Reset(f.f)
			t.drain(ctx, path, f)
		}
	}
}

func (t *FileTailer) drain(ctx context.Context, path string, f *tailedFile) {
	for {
		chunk, err := f.r.ReadSlice('\n')
		f.offset += int64(len(chunk))
		if len(f.line)+len(chunk) <= t.maxBytes {
			f.line = append(f.line, chunk...)
		}
		if err != nil {
			if !errors.Is(err, bufio.ErrBufferFull) {
				// 書きかけの行は、続きが書き足されるのを待つ
				return
			}
			continue
		}
		line := string(trimEOL(f.line))
		f.line = f.line[:0]
		if out, ok := f.joiner.join(line); ok {
			t.receiver.Receive(ctx, observation.LogSourceFile, path, out)
		}
	}
}

func (t *FileTailer) closeAll() {
	for _, f := range t.files {
		f.f.Close()
	}
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
