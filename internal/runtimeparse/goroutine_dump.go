// Package runtimeparse は、pprof の goroutine dump と execution trace を domain の形に読む (domain.RuntimeParser)
package runtimeparse

import (
	"bufio"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// "goroutine 18 [chan receive, 5 minutes]:" / "goroutine 1 gp=0x... m=0 mp=0x... [running]:" /
// "goroutine 7 [select] {handler: /users}:" (Go 1.27 から既定で pprof labels が付く) のいずれにも合わせる
var goroutineHeaderRe = regexp.MustCompile(`^goroutine (\d+)\b[^\[]*\[([^\]]+)\](?: \{(.*)\})?:$`)

var waitMinutesRe = regexp.MustCompile(`^(\d+) minutes?$`)

// Parser は pprof の形式を読む。状態は持たない
type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

// ParseGoroutineDump は /debug/pprof/goroutine?debug=2 (panic 時と同じ形式) を読む。アプリの中で取った dump (ライブラリ型) にも使う
func (p *Parser) ParseGoroutineDump(r io.Reader) ([]model.Goroutine, error) {
	var (
		gs     []model.Goroutine
		cur    *model.Goroutine
		frames []string
		// "created by" の行の次はその位置の行。直前が "created by" だったかを覚えておく
		expectCreatedPos bool
		inCreatedBy      bool
	)
	flush := func() {
		if cur != nil {
			if len(frames) > 0 {
				cur.EntryFunction = model.EntryFunction(frames)
				cur.CurrentFunction = model.CurrentFunction(frames)
			}
			gs = append(gs, *cur)
			cur = nil
		}
		frames = nil
		inCreatedBy = false
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if m := goroutineHeaderRe.FindStringSubmatch(line); m != nil {
			flush()
			id, err := strconv.ParseInt(m[1], 10, 64)
			if err != nil {
				return nil, model.ErrInternal.Wrap(err, "parse goroutine id")
			}
			cur = &model.Goroutine{ID: model.GoroutineID(id)}
			applyGoroutineStatus(cur, m[2])
			cur.Labels = parseLabels(m[3])
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case line == "":
			flush()
		case expectCreatedPos:
			cur.CreatedAt = trimFramePosition(line)
			expectCreatedPos = false
		case strings.HasPrefix(line, "created by "):
			fn, _, _ := strings.Cut(strings.TrimPrefix(line, "created by "), " in goroutine ")
			cur.CreatedBy = fn
			expectCreatedPos = true
			inCreatedBy = true
		case !inCreatedBy && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "..."):
			frames = append(frames, trimFrameArgs(line))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, model.ErrInternal.Wrap(err, "read goroutine dump")
	}
	flush()
	return gs, nil
}

// applyGoroutineStatus は "chan receive, 5 minutes, locked to thread" や "select (leaked)" を読み分ける。
func applyGoroutineStatus(g *model.Goroutine, status string) {
	if s, ok := strings.CutSuffix(status, " (leaked)"); ok {
		g.Leaked = true
		status = s
	}
	parts := strings.Split(status, ", ")
	g.State = parts[0]
	for _, p := range parts[1:] {
		if m := waitMinutesRe.FindStringSubmatch(p); m != nil {
			n, _ := strconv.Atoi(m[1])
			g.Wait = time.Duration(n) * time.Minute
		}
	}
}

// parseLabels は "{handler: /users, scope: checkout}" の中身を読む。値に ", " を含む (引用符で囲まれた) ラベルは正しく分けられない
func parseLabels(s string) map[string]string {
	if s == "" {
		return nil
	}
	labels := map[string]string{}
	for kv := range strings.SplitSeq(s, ", ") {
		k, v, ok := strings.Cut(kv, ": ")
		if !ok {
			continue
		}
		if uk, err := strconv.Unquote(k); err == nil {
			k = uk
		}
		if uv, err := strconv.Unquote(v); err == nil {
			v = uv
		}
		labels[k] = v
	}
	return labels
}

func trimFrameArgs(line string) string {
	if i := strings.LastIndex(line, "("); i > 0 {
		return line[:i]
	}
	return line
}

func trimFramePosition(line string) string {
	pos, _, _ := strings.Cut(strings.TrimSpace(line), " +0x")
	return pos
}
