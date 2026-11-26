// Package internal は、server がアプリのビルドに差し込む agent のソースを埋め込む。
// agent のソースは app/agent と、agent との決まりの protocol/agent の 2 か所にあり、go:embed は親のディレクトリを指せないので、両方を下に持つここに置く。
// server・agent・アプリがお互いを import しないよう、どのコンポーネントにも置かない
package internal

import (
	"embed"
	"io/fs"
)

//go:embed app/agent protocol/agent
var embeddedAgent embed.FS

// EmbeddedAgent は、埋め込んだ agent (app/agent) と agent との決まり (protocol/agent) のファイルを、それぞれの根からのパスと中身で返す
func EmbeddedAgent() (agent map[string][]byte, protocol map[string][]byte, err error) {
	if agent, err = readTree("app/agent"); err != nil {
		return nil, nil, err
	}
	if protocol, err = readTree("protocol/agent"); err != nil {
		return nil, nil, err
	}
	return agent, protocol, nil
}

func readTree(root string) (map[string][]byte, error) {
	sub, err := fs.Sub(embeddedAgent, root)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		out[p] = b
		return nil
	})
	return out, err
}
