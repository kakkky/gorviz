package accessor

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

type BackupFileAccessor struct {
	dir  string
	last atomic.Int64
}

func NewBackupFileAccessor(dir string) *BackupFileAccessor {
	return &BackupFileAccessor{dir: dir}
}

func (a *BackupFileAccessor) Write(ctx context.Context, data []byte) (string, error) {
	// 遅いディスク (ネットワークストレージ) に見立てる
	time.Sleep(1500 * time.Millisecond)
	path := filepath.Join(a.dir, "todo-backup.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", domain.ErrInternal.Wrap(err, "write backup")
	}
	a.last.Store(time.Now().UnixNano())
	return path, nil
}

func (a *BackupFileAccessor) LastWrittenAt(ctx context.Context) (time.Time, bool) {
	n := a.last.Load()
	return time.Unix(0, n), n > 0
}
