// Package docs は cli と mcp に埋め込むドキュメント。server とは別に、手元の版のまま引ける。
// 1 行目の見出しを要約として一覧に出す
package docs

import (
	"embed"
	"io/fs"
	"slices"
	"strings"
)

//go:embed *.md operations/*.md
var files embed.FS

type Doc struct {
	Name    string
	Summary string
}

func List() []Doc {
	var out []Doc
	_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		name := strings.TrimSuffix(p, ".md")
		out = append(out, Doc{Name: name, Summary: Summary(name)})
		return nil
	})
	slices.SortFunc(out, func(a, b Doc) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func Get(name string) (string, bool) {
	b, err := files.ReadFile(name + ".md")
	if err != nil {
		return "", false
	}
	return string(b), true
}

func Summary(name string) string {
	body, ok := Get(name)
	if !ok {
		return ""
	}
	first, _, _ := strings.Cut(body, "\n")
	return strings.TrimSpace(strings.TrimPrefix(first, "#"))
}

// Operation は serverapi の操作の名前に対応するドキュメント
func Operation(name string) (string, bool) {
	return Get("operations/" + name)
}

func OperationSummary(name string) string {
	return Summary("operations/" + name)
}
