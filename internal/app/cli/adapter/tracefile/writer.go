// Package tracefile は録画をディスクに書き出す。録画ごとのディレクトリに、区切りを "go tool trace" で開けるファイルとして置く
package tracefile

import (
	"fmt"
	"os"
	"path/filepath"
)

type Writer struct{}

func NewWriter() *Writer {
	return &Writer{}
}

// Create は out/<id> のディレクトリを作り、そのパスを返す
func (w *Writer) Create(out, id string) (string, error) {
	dir := filepath.Join(out, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// WriteChunk は dir/chunk-<n>.trace に書く
func (w *Writer) WriteChunk(dir string, n int, raw []byte) error {
	return os.WriteFile(filepath.Join(dir, fmt.Sprintf("chunk-%d.trace", n)), raw, 0o644)
}
