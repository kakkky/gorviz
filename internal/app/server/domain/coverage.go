package domain

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
)

// CoverageBlock は go tool covdata textfmt が書き出した 1 行。File はパッケージのパスとファイル名
type CoverageBlock struct {
	File      string
	StartLine int
	EndLine   int
	Stmts     int
	Count     int
}

// ParseCoverageProfile は text 形式の coverage を読む。agent のものは除く
func ParseCoverageProfile(text []byte) ([]CoverageBlock, error) {
	var out []CoverageBlock
	sc := bufio.NewScanner(bytes.NewReader(text))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "mode:") || line == "" {
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			return nil, ErrInvalidArgument.With("read coverage profile line " + line)
		}
		file, rest := line[:colon], line[colon+1:]
		var sl, sc2, el, ec, stmts, count int
		if _, err := fmt.Sscanf(rest, "%d.%d,%d.%d %d %d", &sl, &sc2, &el, &ec, &stmts, &count); err != nil {
			return nil, ErrInvalidArgument.Wrap(err, "read coverage profile line")
		}
		if strings.Contains(file, "/"+AgentPackage+"/") {
			continue
		}
		out = append(out, CoverageBlock{File: file, StartLine: sl, EndLine: el, Stmts: stmts, Count: count})
	}
	if err := sc.Err(); err != nil {
		return nil, ErrInvalidArgument.Wrap(err, "read coverage profile")
	}
	return out, nil
}

func CoverageFilesOf(blocks []CoverageBlock) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range blocks {
		if !seen[b.File] {
			seen[b.File] = true
			out = append(out, b.File)
		}
	}
	slices.Sort(out)
	return out
}

type LineRange struct {
	From int
	To   int
}

func (r LineRange) String() string {
	if r.From == r.To {
		return strconv.Itoa(r.From)
	}
	return strconv.Itoa(r.From) + "-" + strconv.Itoa(r.To)
}

type FunctionCoverage struct {
	Function  string
	File      string
	Percent   float64
	Uncovered []LineRange
}

// CoverageReport は診断 1 回で通った行。通っていない行は関数ごとに範囲でまとめる。一度も通らなかった関数は名前だけを並べる
type CoverageReport struct {
	Percent    float64
	Statements int
	Covered    int
	Partial    []FunctionCoverage
	Unreached  []string
}

type functionSpan struct {
	name     string
	from, to int
}

func functionsIn(src []byte) []functionSpan {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var out []functionSpan
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			name = "(" + recvName(fn.Recv.List[0].Type) + ")." + name
		}
		out = append(out, functionSpan{name: name, from: fset.Position(fn.Pos()).Line, to: fset.Position(fn.End()).Line})
	}
	return out
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return "*" + recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

// BuildCoverageReport は coverage の行を、ソースの関数の範囲に当てはめる。sources はファイル (CoverageBlock の File) ごとのソース
func BuildCoverageReport(blocks []CoverageBlock, sources map[string][]byte) CoverageReport {
	type acc struct {
		fc         FunctionCoverage
		stmts, hit int
		uncovered  []LineRange
	}
	var r CoverageReport
	funcs := map[string]*acc{}
	var order []string
	spans := map[string][]functionSpan{}
	for file, src := range sources {
		spans[file] = functionsIn(src)
	}
	for _, b := range blocks {
		r.Statements += b.Stmts
		if b.Count > 0 {
			r.Covered += b.Stmts
		}
		name := path.Base(path.Dir(b.File)) + ".?"
		for _, s := range spans[b.File] {
			if s.from <= b.StartLine && b.StartLine <= s.to {
				name = path.Dir(b.File) + "." + s.name
				break
			}
		}
		a, ok := funcs[name]
		if !ok {
			a = &acc{fc: FunctionCoverage{Function: name, File: b.File}}
			funcs[name] = a
			order = append(order, name)
		}
		a.stmts += b.Stmts
		if b.Count > 0 {
			a.hit += b.Stmts
		} else if b.Stmts > 0 {
			a.uncovered = append(a.uncovered, LineRange{From: b.StartLine, To: b.EndLine})
		}
	}
	if r.Statements > 0 {
		r.Percent = float64(r.Covered) * 100 / float64(r.Statements)
	}
	for _, name := range order {
		a := funcs[name]
		if a.stmts == 0 {
			continue
		}
		if a.hit == 0 {
			r.Unreached = append(r.Unreached, name)
			continue
		}
		if a.hit == a.stmts {
			continue
		}
		a.fc.Percent = float64(a.hit) * 100 / float64(a.stmts)
		a.fc.Uncovered = mergeRanges(a.uncovered)
		r.Partial = append(r.Partial, a.fc)
	}
	slices.SortFunc(r.Partial, func(a, b FunctionCoverage) int { return cmp.Compare(a.Function, b.Function) })
	slices.Sort(r.Unreached)
	return r
}

func mergeRanges(rs []LineRange) []LineRange {
	slices.SortFunc(rs, func(a, b LineRange) int { return cmp.Compare(a.From, b.From) })
	var out []LineRange
	for _, r := range rs {
		if n := len(out); n > 0 && r.From <= out[n-1].To+1 {
			out[n-1].To = max(out[n-1].To, r.To)
			continue
		}
		out = append(out, r)
	}
	return out
}
