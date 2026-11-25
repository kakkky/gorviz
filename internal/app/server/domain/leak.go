package domain

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// LeakProfileStack は goroutineleak プロファイルの 1 つのスタック。Count はそのスタックでリークした goroutine の数
type LeakProfileStack struct {
	Count  int
	Frames []Frame
}

// ParseGoroutineLeakProfile は debug=1 の goroutineleak プロファイルを読む。
// "N @ 0x..." の行がスタックの始まりで、続く "#\t0x...\tfunc+0x..\tfile:line" の行がフレーム
func ParseGoroutineLeakProfile(text []byte) ([]LeakProfileStack, error) {
	var out []LeakProfileStack
	sc := bufio.NewScanner(bytes.NewReader(text))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "goroutineleak profile:"):
		case strings.Contains(line, " @ "):
			n, err := strconv.Atoi(strings.TrimSpace(line[:strings.Index(line, " @ ")]))
			if err != nil {
				return nil, ErrInvalidArgument.Wrap(err, "read goroutineleak profile count")
			}
			out = append(out, LeakProfileStack{Count: n})
		case strings.HasPrefix(line, "#") && len(out) > 0:
			fields := strings.Split(strings.TrimPrefix(line, "#"), "\t")
			var parts []string
			for _, f := range fields {
				if f = strings.TrimSpace(f); f != "" {
					parts = append(parts, f)
				}
			}
			if len(parts) < 3 {
				continue
			}
			fn := parts[1]
			if i := strings.LastIndex(fn, "+0x"); i > 0 {
				fn = fn[:i]
			}
			file, lineNo := parts[2], 0
			if i := strings.LastIndex(file, ":"); i > 0 {
				lineNo, _ = strconv.Atoi(file[i+1:])
				file = file[:i]
			}
			last := &out[len(out)-1]
			last.Frames = append(last.Frames, Frame{Function: fn, File: file, Line: lineNo})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, ErrInvalidArgument.Wrap(err, "read goroutineleak profile")
	}
	return out, nil
}

type leakCandidate struct {
	g     *GoroutineRecord
	since time.Duration
	why   string
}

func placeKey(f Frame) string {
	return f.Function + "@" + f.Location()
}

// MatchLeaks はプロファイルのスタックごとの件数を、同じ時刻に同じ場所で待っていた goroutine と突き合わせ、生成元ごとの Detection にする。
// 同じ場所で待つ goroutine がちょうど件数だけあれば全部をリークとし (FACT)、多ければ待っている時間が長い順に件数分を選ぶ (INFERENCE)
func MatchLeaks(ix *TraceIndex, at time.Duration, stacks []LeakProfileStack) []Detection {
	waiting := map[string][]leakCandidate{}
	for _, id := range ix.GoroutineIDs() {
		g, _ := ix.Goroutine(id)
		if !g.App() || (g.Ended && g.EndedAt <= at) {
			continue
		}
		c, ok := ix.StateAt(g, at)
		if !ok || c.Place != PlaceWaiting {
			continue
		}
		f := CurrentFrame(ix.Stack(c.Stack))
		waiting[placeKey(f)] = append(waiting[placeKey(f)], leakCandidate{g: g, since: c.At, why: ix.Reason(c.Reason)})
	}
	byCreator := map[string]*Detection{}
	var order []string
	for _, s := range stacks {
		if s.Count <= 0 || len(s.Frames) == 0 {
			continue
		}
		blocked := CurrentFrame(s.Frames)
		if IsAgentFunction(blocked.Function) || IsAgentFunction(EntryFunction(s.Frames)) {
			continue
		}
		cands := slices.Clone(waiting[placeKey(blocked)])
		slices.SortFunc(cands, func(a, b leakCandidate) int {
			return cmp.Or(cmp.Compare(a.since, b.since), cmp.Compare(a.g.ID, b.g.ID))
		})
		ls := LeakStack{Blocked: blocked, Count: s.Count, Of: max(len(cands), s.Count), Certainty: CertaintyFact}
		picked := cands
		if len(cands) > s.Count {
			picked = cands[:s.Count]
			ls.Certainty = CertaintyInference
		}
		for _, c := range picked {
			ls.Goroutines = append(ls.Goroutines, c.g.ID)
		}
		if len(picked) > 0 {
			ls.Creator, ls.Reason = picked[0].g.Creator, picked[0].why
		}
		if ls.Creator.Function == "" {
			ls.Creator = Frame{Function: EntryFunction(s.Frames)}
		}
		target := IssueFunction(ls.Creator.Function)
		d, ok := byCreator[target]
		if !ok {
			d = &Detection{Kind: IssueGoroutineLeak, Target: target, Certainty: CertaintyFact, From: at, To: at}
			byCreator[target] = d
			order = append(order, target)
		}
		d.Leaks = append(d.Leaks, ls)
	}
	out := make([]Detection, 0, len(order))
	for _, t := range order {
		d := byCreator[t]
		n := 0
		for _, l := range d.Leaks {
			n += l.Count
		}
		d.Value = fmt.Sprintf("%d goroutines leaked", n)
		out = append(out, *d)
	}
	return out
}

// LeakOf は、goroutine がリークの判定に入っているか
func LeakOf(leaks []LeakStack, g GoroutineID) (LeakStack, bool) {
	for _, l := range leaks {
		if slices.Contains(l.Goroutines, g) {
			return l, true
		}
	}
	return LeakStack{}, false
}
