package client

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/domain"
)

// "goroutine 18 [chan receive, 5 minutes]:" / "goroutine 1 gp=0x... m=0 mp=0x... [running]:" /
// "goroutine 7 [select] {handler: /users}:" (Go 1.27 から既定で pprof labels が付く) のいずれにも合わせる
var goroutineHeaderRe = regexp.MustCompile(`^goroutine (\d+)\b[^\[]*\[([^\]]+)\](?: \{.*\})?:$`)

var waitMinutesRe = regexp.MustCompile(`^(\d+) minutes?$`)

// parseGoroutineDump は /debug/pprof/goroutine?debug=2 (panic 時と同じ形式) を読む。
func parseGoroutineDump(r io.Reader) ([]domain.Goroutine, error) {
	var (
		gs     []domain.Goroutine
		cur    *domain.Goroutine
		frames []string
		// "created by" の行の次はその位置の行。直前が "created by" だったかを覚えておく
		expectCreatedPos bool
		inCreatedBy      bool
	)
	flush := func() {
		if cur != nil {
			if len(frames) > 0 {
				cur.EntryFunction = frames[len(frames)-1]
				cur.CurrentFunction = domain.CurrentFunction(frames)
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
				return nil, domain.ErrInternal.Wrap(err, "parse goroutine id")
			}
			cur = &domain.Goroutine{ID: domain.GoroutineID(id)}
			applyGoroutineStatus(cur, m[2])
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
		return nil, domain.ErrInternal.Wrap(err, "read goroutine dump")
	}
	flush()
	return gs, nil
}

// applyGoroutineStatus は "chan receive, 5 minutes, locked to thread" や "select (leaked)" を読み分ける。
func applyGoroutineStatus(g *domain.Goroutine, status string) {
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
