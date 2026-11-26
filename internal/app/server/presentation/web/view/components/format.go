package components

import (
	"path"
	"strings"
)

// ShortPath は文字列の中のファイルパス ("/Users/me/app/cmd/main.go:57") を、親のディレクトリとファイル名 ("cmd/main.go:57") に縮める。
// 関数名のパッケージパスは、先頭がドメインのモジュール ("github.com/me/app/internal/agent.(*Agent).Run") なら最後の部分 ("agent.(*Agent).Run") に縮め、
// 標準ライブラリ ("net/http.(*Server).Serve") はそのまま残す。画面では縮めたものを出し、元の文字列は title に入れる
func ShortPath(s string) string {
	words := strings.Split(s, " ")
	for i, w := range words {
		if !strings.Contains(w, "/") {
			continue
		}
		if !strings.Contains(w, ".go") {
			if first, _, _ := strings.Cut(w, "/"); strings.Contains(first, ".") {
				words[i] = path.Base(w)
			}
			continue
		}
		dir, file := path.Split(w)
		if parent := path.Base(strings.TrimSuffix(dir, "/")); parent != "." && parent != "/" && dir != "" {
			file = parent + "/" + file
		}
		words[i] = file
	}
	return strings.Join(words, " ")
}
