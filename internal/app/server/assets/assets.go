// Package assets は GUI の静的ファイル (esbuild で書き出した JS・CSS) と、テンプレートから参照する controller・アイランドの宣言
package assets

//go:generate go run ./tsgen -out src/gen/types.ts

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
)

// URLPrefix は dist/ のファイルを配る URL の頭
const URLPrefix = "/assets/"

//go:embed dist
var dist embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

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

// Versioned は dist/ のファイルの URL に、中身から作った版を付ける
func Versioned(path string) string {
	return path + "?v=" + version
}

// StylesheetPath は全画面の CSS
const StylesheetPath = URLPrefix + "main.css"
