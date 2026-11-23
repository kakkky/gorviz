package domain

import (
	"bufio"
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

var (
	// "3 @ 0x100f88428 0x100f1c670"
	leakStackRe = regexp.MustCompile(`^(\d+) @(?: 0x[0-9a-f]+)*$`)
	// "#	0x10102b64f	main.leak+0x2f	/app/main.go:14"
	leakFrameRe = regexp.MustCompile(`^#\s+0x[0-9a-f]+\s+(\S+?)(?:\+0x[0-9a-f]+)?\s+(.+):(\d+)$`)
)

// ParseGoroutineLeakProfile は goroutineleak プロファイルの debug=1 (スタックごとの件数) を読む
func ParseGoroutineLeakProfile(profile []byte, at time.Time) (observation.GoroutineLeakCheck, error) {
	check := observation.GoroutineLeakCheck{At: at, Status: observation.LeakCheckDone}
	sc := bufio.NewScanner(bytes.NewReader(profile))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	header := false
	var cur *observation.LeakedStack
	flush := func() {
		if cur != nil && cur.Count > 0 {
			check.Leaks = append(check.Leaks, *cur)
		}
		cur = nil
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if !header {
			if !strings.HasPrefix(line, "goroutineleak profile:") {
				return observation.GoroutineLeakCheck{}, ErrInvalidArgument.With("not a goroutineleak profile")
			}
			header = true
			continue
		}
		if m := leakStackRe.FindStringSubmatch(line); m != nil {
			flush()
			n, err := strconv.Atoi(m[1])
			if err != nil {
				return observation.GoroutineLeakCheck{}, ErrInvalidArgument.Wrap(err, "parse leaked goroutine count")
			}
			cur = &observation.LeakedStack{Count: n}
			continue
		}
		if cur == nil {
			continue
		}
		if m := leakFrameRe.FindStringSubmatch(line); m != nil {
			ln, _ := strconv.Atoi(m[3])
			cur.Stack = append(cur.Stack, observation.Frame{Function: m[1], File: m[2], Line: ln})
		}
	}
	if err := sc.Err(); err != nil {
		return observation.GoroutineLeakCheck{}, ErrInvalidArgument.Wrap(err, "read goroutineleak profile")
	}
	if !header {
		return observation.GoroutineLeakCheck{}, ErrInvalidArgument.With("empty goroutineleak profile")
	}
	flush()
	return check, nil
}

func UnsupportedLeakCheck(at time.Time) observation.GoroutineLeakCheck {
	return observation.GoroutineLeakCheck{At: at, Status: observation.LeakCheckUnsupported}
}

type LeakCheckPlan int

const (
	LeakCheckSkip LeakCheckPlan = iota
	LeakCheckFetch
	// LeakCheckReportDisabled は切ってあることを、インスタンスごとに 1 回だけ知らせる
	LeakCheckReportDisabled
)

// PlanLeakCheck は取るたびに GC が 1 回走るので、間隔を空けて取る。間隔が 0 なら取らない
func PlanLeakCheck(last, now time.Time, interval time.Duration) LeakCheckPlan {
	switch {
	case interval == 0 && last.IsZero():
		return LeakCheckReportDisabled
	case interval == 0:
		return LeakCheckSkip
	case last.IsZero() || now.Sub(last) >= interval:
		return LeakCheckFetch
	}
	return LeakCheckSkip
}

func DisabledLeakCheck(at time.Time) observation.GoroutineLeakCheck {
	return observation.GoroutineLeakCheck{At: at, Status: observation.LeakCheckDisabled}
}
