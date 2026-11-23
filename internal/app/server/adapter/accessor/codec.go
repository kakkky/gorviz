package accessor

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"log/slog"
	"strconv"

	"google.golang.org/protobuf/proto"

	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
)

// checkedLine は JSON 1 件の前に CRC32 を付けた 1 行。.jsonl の行と、置き換えて書くファイル全体に使う
func checkedLine(v any) ([]byte, error) {
	js, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, "%08x %s\n", crc32.ChecksumIEEE(js), js), nil
}

func checkedLines[T any](items []T) ([]byte, error) {
	var buf []byte
	for _, it := range items {
		b, err := checkedLine(it)
		if err != nil {
			return nil, err
		}
		buf = append(buf, b...)
	}
	return buf, nil
}

var errChecksum = errors.New("checksum mismatch")

func decodeChecked(line []byte, v any) error {
	if len(line) < 10 || line[8] != ' ' {
		return errChecksum
	}
	sum, err := strconv.ParseUint(string(line[:8]), 16, 32)
	if err != nil || uint32(sum) != crc32.ChecksumIEEE(line[9:]) {
		return errChecksum
	}
	return json.Unmarshal(line[9:], v)
}

// eachLine は name の行を読む。途中で切れた末尾の 1 行は書きかけなので読まない。チェックサムの合わない行は飛ばして警告する
func eachLine[T any](root *filesystem.Root, name string, fn func(T)) error {
	b, err := root.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			break
		}
		line := b[:i]
		b = b[i+1:]
		var v T
		if err := decodeChecked(line, &v); err != nil {
			slog.Warn("skip a broken line", slog.String("file", name), slog.Any("err", err))
			continue
		}
		fn(v)
	}
	return nil
}

func readLines[T any](root *filesystem.Root, name string) ([]T, error) {
	var out []T
	err := eachLine(root, name, func(v T) { out = append(out, v) })
	return out, err
}

// readChecked は置き換えて書いたファイルを読む。なければ false
func readChecked(root *filesystem.Root, name string, v any) (bool, error) {
	b, err := root.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := decodeChecked(bytes.TrimSuffix(b, []byte("\n")), v); err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}
	return true, nil
}

// validLines は最後の改行までを残す
func validLines(f io.ReaderAt, size int64) (int64, error) {
	const block = 64 << 10
	buf := make([]byte, block)
	for end := size; end > 0; {
		start := max(end-block, 0)
		n, err := f.ReadAt(buf[:end-start], start)
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
			return start + int64(i) + 1, nil
		}
		end = start
	}
	return 0, nil
}

// gzipMember は protobuf 1 件を長さ付きで 1 つの gzip にする。gzip をつなげて追記しても、1 件ずつ取り出せるようにするため
func gzipMember(m proto.Message) ([]byte, error) {
	b, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(binary.AppendUvarint(nil, uint64(len(b)))); err != nil {
		return nil, err
	}
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// eachMember は gzip を 1 つずつ開き、中の protobuf と、その gzip の終わりの位置を渡す。
// 壊れた gzip (書きかけ・化けたもの) があれば、そこで止めて壊れた位置を返す
func eachMember(b []byte, fn func(msg []byte, end int64) error) (int64, error) {
	br := bytes.NewReader(b)
	var good int64
	for br.Len() > 0 {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return good, err
		}
		zr.Multistream(false)
		raw, err := io.ReadAll(zr)
		if err != nil {
			return good, err
		}
		n, k := binary.Uvarint(raw)
		if k <= 0 || uint64(len(raw)-k) != n {
			return good, errChecksum
		}
		good = int64(len(b) - br.Len())
		if err := fn(raw[k:], good); err != nil {
			return good, err
		}
	}
	return good, nil
}

func validMembers(f io.ReaderAt, size int64) (int64, error) {
	b := make([]byte, size)
	if _, err := f.ReadAt(b, 0); err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	good, _ := eachMember(b, func([]byte, int64) error { return nil })
	return good, nil
}

// readMembers は name の gzip を順に読む。途中で壊れていたら、そこまでを返して警告する
func readMembers[M proto.Message](root *filesystem.Root, name string, newMsg func() M, fn func(M)) error {
	b, err := root.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	good, err := eachMember(b, func(raw []byte, _ int64) error {
		m := newMsg()
		if err := proto.Unmarshal(raw, m); err != nil {
			return err
		}
		fn(m)
		return nil
	})
	if err != nil && good < int64(len(b)) {
		slog.Warn("skip the broken rest of a file", slog.String("file", name), slog.Int64("from", good), slog.Any("err", err))
	}
	return nil
}

func warnBroken(file string, err error) {
	slog.Warn("skip a broken entry", slog.String("file", file), slog.Any("err", err))
}

// appendLines は items を、fileOf で決めたファイルごとにまとめて追記する
func appendLines[T any](root *filesystem.Root, items []T, fileOf func(T) string) error {
	byFile := map[string][]T{}
	var order []string
	for _, it := range items {
		f := fileOf(it)
		if _, ok := byFile[f]; !ok {
			order = append(order, f)
		}
		byFile[f] = append(byFile[f], it)
	}
	for _, f := range order {
		b, err := checkedLines(byFile[f])
		if err != nil {
			return err
		}
		if _, err := root.Append(f, b, validLines); err != nil {
			return err
		}
	}
	return nil
}

func appendMembers[T any](root *filesystem.Root, items []T, fileOf func(T) string, toMsg func(T) proto.Message) error {
	for _, it := range items {
		b, err := gzipMember(toMsg(it))
		if err != nil {
			return err
		}
		if _, err := root.Append(fileOf(it), b, validMembers); err != nil {
			return err
		}
	}
	return nil
}
