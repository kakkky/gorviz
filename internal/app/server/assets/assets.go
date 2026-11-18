package assets

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
)

//go:embed dist
var dist embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// version は静的ファイルの中身から作る版。中身が変わると URL (Versioned) が変わる
var version = func() string {
	h := sha256.New()
	err := fs.WalkDir(FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(FS(), p)
		if err != nil {
			return err
		}
		h.Write([]byte(p))
		h.Write(b)
		return nil
	})
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}()

// Versioned は静的ファイルの URL に版を付ける。Turbo は画面を移っても同じ URL の CSS と JS を読み直さないので、
// 作り直したあとも古いものを使い続けないよう、中身が変わったら URL を変える
func Versioned(path string) string {
	return path + "?v=" + version
}
