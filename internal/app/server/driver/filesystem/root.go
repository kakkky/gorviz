// Package filesystem は保存先のディレクトリを開き、その中だけを読み書きさせる
package filesystem

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// FormatVersion は保存先のファイルの形式のバージョン
const FormatVersion = 1

const (
	formatVersionFile = "format-version"
	lockFile          = "lock"
	tempMarker        = ".tmp-"
)

// Root は保存先。ロックを持ち、パスは保存先の中に閉じる (os.Root)。
// 追記は path ごとに 1 つずつにし、プロセスで最初に追記するときに途中で切れた末尾を切り詰める
type Root struct {
	root  *os.Root
	lock  *os.File
	mu    sync.Mutex
	locks map[string]*sync.Mutex
	fresh map[string]bool
}

// NewRoot は保存先を開き、ロックを取り、形式のバージョンを確かめ、書きかけの一時ファイルを消す
func NewRoot(dir string) (*Root, func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create the data directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("open the data directory: %w", err)
	}
	lock, err := root.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		root.Close()
		return nil, nil, fmt.Errorf("open the lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		root.Close()
		return nil, nil, fmt.Errorf("another gorviz-server uses %s: %w", dir, err)
	}
	r := &Root{root: root, lock: lock, locks: map[string]*sync.Mutex{}, fresh: map[string]bool{}}
	cleanup := func() {
		syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		lock.Close()
		root.Close()
	}
	if err := r.checkFormat(); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := r.removeTemps(); err != nil {
		cleanup()
		return nil, nil, err
	}
	return r, cleanup, nil
}

func (r *Root) checkFormat() error {
	b, err := r.root.ReadFile(formatVersionFile)
	if errors.Is(err, fs.ErrNotExist) {
		return r.Replace(formatVersionFile, []byte(strconv.Itoa(FormatVersion)+"\n"))
	}
	if err != nil {
		return fmt.Errorf("read the format version: %w", err)
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return fmt.Errorf("the format version is broken: %q", b)
	}
	if v > FormatVersion {
		return fmt.Errorf("the data directory has format version %d, newer than this server (%d)", v, FormatVersion)
	}
	// バージョン 1 より古い形式はないので、移すものはない
	if v < FormatVersion {
		return r.Replace(formatVersionFile, []byte(strconv.Itoa(FormatVersion)+"\n"))
	}
	return nil
}

func (r *Root) removeTemps() error {
	return fs.WalkDir(r.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.Contains(d.Name(), tempMarker) {
			return r.root.Remove(p)
		}
		return nil
	})
}

func (r *Root) pathLock(name string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.locks[name]
	if !ok {
		m = &sync.Mutex{}
		r.locks[name] = m
	}
	return m
}

// Lock は key ごとの排他。読んでから書き直すファイルや、1 つずつ書き込むインスタンスに使う
func (r *Root) Lock(key string) func() {
	m := r.pathLock("lock:" + key)
	m.Lock()
	return m.Unlock
}

// ValidLength は追記するファイルのうち、壊れていない先頭の長さを返す
type ValidLength func(f io.ReaderAt, size int64) (int64, error)

// Append は name の末尾に data を足し、足した位置を返す。プロセスで最初に足すときは、valid で末尾の切れた分を切り詰めてから足す
func (r *Root) Append(name string, data []byte, valid ValidLength) (int64, error) {
	m := r.pathLock(name)
	m.Lock()
	defer m.Unlock()
	if err := r.root.MkdirAll(path.Dir(name), 0o755); err != nil {
		return 0, err
	}
	f, err := r.root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()
	r.mu.Lock()
	checked := r.fresh[name]
	r.mu.Unlock()
	if !checked && size > 0 && valid != nil {
		n, err := valid(f, size)
		if err != nil {
			return 0, err
		}
		if n < size {
			if err := f.Truncate(n); err != nil {
				return 0, err
			}
			size = n
		}
	}
	if _, err := f.WriteAt(data, size); err != nil {
		return 0, err
	}
	r.mu.Lock()
	r.fresh[name] = true
	r.mu.Unlock()
	return size, nil
}

func (r *Root) writeTemp(name string, data []byte) (string, error) {
	if err := r.root.MkdirAll(path.Dir(name), 0o755); err != nil {
		return "", err
	}
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tmp := name + tempMarker + hex.EncodeToString(b[:])
	f, err := r.root.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		r.root.Remove(tmp)
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		r.root.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		r.root.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// Replace は一時ファイルに書いてから name と置き換える
func (r *Root) Replace(name string, data []byte) error {
	tmp, err := r.writeTemp(name, data)
	if err != nil {
		return err
	}
	if err := r.root.Rename(tmp, name); err != nil {
		r.root.Remove(tmp)
		return err
	}
	return nil
}

// Create は一時ファイルに書いてから name を付ける。もうあれば何もせず false を返す
func (r *Root) Create(name string, data []byte) (bool, error) {
	m := r.pathLock(name)
	m.Lock()
	defer m.Unlock()
	if _, err := r.root.Stat(name); err == nil {
		return false, nil
	}
	if err := r.Replace(name, data); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Root) ReadFile(name string) ([]byte, error) {
	return r.root.ReadFile(name)
}

// ReadAt は name の [off, off+n) を読む
func (r *Root) ReadAt(name string, off, n int64) ([]byte, error) {
	f, err := r.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, n)
	if _, err := f.ReadAt(b, off); err != nil {
		return nil, err
	}
	return b, nil
}

func (r *Root) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(r.root.FS(), name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}

func (r *Root) Stat(name string) (fs.FileInfo, error) {
	return r.root.Stat(name)
}

// Walk は dir の下のファイルを辿る。dir がなければ何もしない
func (r *Root) Walk(dir string, fn func(p string, info fs.FileInfo) error) error {
	err := fs.WalkDir(r.root.FS(), dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return fn(p, info)
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// RemoveAll は name (ファイルかディレクトリ) を消し、空になった親のディレクトリを stop の手前まで消す
func (r *Root) RemoveAll(name, stop string) error {
	m := r.pathLock(name)
	m.Lock()
	err := r.root.RemoveAll(name)
	m.Unlock()
	r.mu.Lock()
	delete(r.fresh, name)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	for dir := path.Dir(name); dir != "." && dir != stop && strings.HasPrefix(dir, stop); dir = path.Dir(dir) {
		entries, err := fs.ReadDir(r.root.FS(), dir)
		if err != nil || len(entries) > 0 {
			break
		}
		if err := r.root.Remove(dir); err != nil {
			break
		}
	}
	return nil
}
