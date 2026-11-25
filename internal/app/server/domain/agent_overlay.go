package domain

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

const (
	agentImportPath       = "github.com/kakkky/gorviz/internal/app/agent"
	agentsocketImportPath = "github.com/kakkky/gorviz/internal/protocol/agentsocket"
)

// SourceFile は Go のソースのファイル 1 つ。Path は、agent のソースなら agent の根からの相対パス、アプリのソースなら絶対パス
type SourceFile struct {
	Path string
	Data []byte
}

// MainPackage は agent を差し込むアプリの main パッケージ
type MainPackage struct {
	ImportPath string
	Dir        string
	Files      []SourceFile
}

// Overlay は go build -overlay に渡す、置き換える (足す) ファイルの絶対パスと中身
type Overlay map[string][]byte

// AgentOverlay は agent と agentsocket のソースを、アプリの main パッケージの下の gorvizagent/ として見せ、import パスをそこへ書き換える。
// main には agent を import して、main が戻るときに agent を止める defer を足す。どちらも元の行に書き足し、行番号をずらさない
func AgentOverlay(main MainPackage, agent, socket []SourceFile) (Overlay, error) {
	base := main.ImportPath + "/" + AgentPackage
	rewrite := func(b []byte) []byte {
		b = bytes.ReplaceAll(b, []byte(`"`+agentsocketImportPath+`"`), []byte(`"`+base+`/agentsocket"`))
		return bytes.ReplaceAll(b, []byte(`"`+agentImportPath), []byte(`"`+base))
	}
	ov := Overlay{}
	dir := filepath.Join(main.Dir, AgentPackage)
	for _, f := range agent {
		if skipAgentSource(f.Path) {
			continue
		}
		ov[filepath.Join(dir, filepath.FromSlash(f.Path))] = rewrite(f.Data)
	}
	for _, f := range socket {
		if skipAgentSource(f.Path) {
			continue
		}
		ov[filepath.Join(dir, "agentsocket", filepath.FromSlash(f.Path))] = rewrite(f.Data)
	}
	for _, f := range main.Files {
		patched, ok, err := patchMain(f.Data, base)
		if err != nil {
			return nil, ErrInvalidArgument.Wrap(err, "parse "+f.Path)
		}
		if ok {
			ov[f.Path] = patched
			return ov, nil
		}
	}
	return nil, ErrInvalidArgument.With("no func main in package " + main.ImportPath + "; pass a main package, like gorviz run ./cmd/api")
}

func skipAgentSource(p string) bool {
	return !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.HasPrefix(p, "testhelper/")
}

func patchMain(src []byte, agentImport string) ([]byte, bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, false, err
	}
	var body *ast.BlockStmt
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "main" && fn.Body != nil {
			body = fn.Body
		}
	}
	if body == nil {
		return nil, false, nil
	}
	importAt := fset.Position(f.Name.End()).Offset
	deferAt := fset.Position(body.Lbrace).Offset + 1
	var out bytes.Buffer
	out.Write(src[:importAt])
	out.WriteString(`; import ` + AgentPackage + ` "` + agentImport + `"`)
	out.Write(src[importAt:deferAt])
	out.WriteString(` defer ` + AgentPackage + `.Stop();`)
	out.Write(src[deferAt:])
	return out.Bytes(), true, nil
}
