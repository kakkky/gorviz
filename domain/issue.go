package domain

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// IssueKind は Issue の種類。どの種類も同じ形 (種類・確かさ・推移・場所・調べに入る先) で見せる
type IssueKind string

const (
	IssueLeak           IssueKind = "LEAK"
	IssueErrorSpike     IssueKind = "ERROR_SPIKE"
	IssueSlowRequest    IssueKind = "SLOW_REQUEST"
	IssueCPUWaitSpike   IssueKind = "CPU_WAIT_SPIKE"
	IssueMemoryPressure IssueKind = "MEMORY_PRESSURE"
)

// Issue は gorviz が検出した、続いている傾向 1 件。
// Goroutine / At は調べに入る先の代表的な G とその時点 (なければ HasGoroutine が false)。
// From は Goroutine 画面で見る区間の始まり (所要時間のあるログならリクエストの始まり)
type Issue struct {
	Kind      IssueKind
	Certainty Certainty
	// Subject は 1 件の単位 (生成元・エラーの種類・時間帯など) を表す文字列
	Subject string
	Summary string
	// Location は関係する場所 (file:line)
	Location     string
	Trend        []int
	Goroutine    GoroutineID
	HasGoroutine bool
	At           time.Time
	From         time.Time
	// LogQuery は Logs で絞り込むときの文字列
	LogQuery string
}

type IssuePolicy struct {
	// ErrorSpikePerMinute 以上同じ種類のエラーが直近 1 分に出たら FACT。超えなくても普段の 3 倍を超えたら INFERENCE
	ErrorSpikePerMinute int
	SlowRequest         time.Duration
	// CPUWaitRatio は、P を待っている G の数の平均がこれ以上なら CPU_WAIT_SPIKE (FACT)
	CPUWaitRatio float64
	// GCPerSecond は、1 秒あたりの GC の回数がこれ以上なら MEMORY_PRESSURE (FACT)
	GCPerSecond float64
}

var (
	uuidRe   = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	hexRe    = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	numberRe = regexp.MustCompile(`\d+`)
)

// MessageShape はメッセージの数値や ID を伏せた形。同じ形のエラーを 1 つの種類にまとめるのに使う
func MessageShape(msg string) string {
	msg = uuidRe.ReplaceAllString(msg, "<id>")
	msg = hexRe.ReplaceAllString(msg, "<hex>")
	return numberRe.ReplaceAllString(msg, "<n>")
}

type logLocator func(LogLine) (GoroutineID, time.Time, bool)

// DetectErrorSpikes は同じ形のエラーログを数え、直近 1 分の件数で判定する。
// 普段 (保持しているログの、直近 1 分より前の 1 分あたりの平均) との比較は INFERENCE にする
func DetectErrorSpikes(lines []LogLine, now time.Time, policy IssuePolicy, locate logLocator) []Issue {
	type shape struct {
		recent, before int
		first, latest  LogLine
		perMinute      map[int64]int
	}
	shapes := map[string]*shape{}
	recentFrom := now.Add(-time.Minute)
	for _, l := range lines {
		if l.Level != LogLevelError {
			continue
		}
		key := MessageShape(l.Message)
		s, ok := shapes[key]
		if !ok {
			s = &shape{first: l, perMinute: map[int64]int{}}
			shapes[key] = s
		}
		s.perMinute[l.At.Unix()/60]++
		if l.At.After(recentFrom) {
			s.recent++
			s.latest = l
		} else {
			s.before++
		}
	}

	var out []Issue
	for key, s := range shapes {
		if s.recent == 0 {
			continue
		}
		certainty, ok := CertaintyFact, s.recent >= policy.ErrorSpikePerMinute
		if !ok {
			minutes := max(int(recentFrom.Sub(s.first.At).Minutes()), 1)
			baseline := float64(s.before) / float64(minutes)
			certainty, ok = CertaintyInference, s.before > 0 && s.recent >= 3 && float64(s.recent) > 3*baseline
		}
		if !ok {
			continue
		}
		issue := Issue{
			Kind: IssueErrorSpike, Certainty: certainty, Subject: key, LogQuery: s.latest.Message,
			Summary: plural(s.recent, "error") + " in the last minute",
			Trend:   minuteTrend(s.perMinute, now, 10),
		}
		issue.Goroutine, issue.At, issue.HasGoroutine = locate(s.latest)
		issue.From = issue.At.Add(-defaultIssueWindow)
		out = append(out, issue)
	}
	return out
}

// DetectSlowRequests はログに載った所要時間が閾値を超えたリクエストを、メッセージの形ごとにまとめる。
// 固定の閾値で判定するので FACT。代表は一番遅かったリクエスト
func DetectSlowRequests(lines []LogLine, now time.Time, policy IssuePolicy, locate logLocator) []Issue {
	type shape struct {
		count     int
		slowest   LogLine
		perMinute map[int64]int
	}
	shapes := map[string]*shape{}
	for _, l := range lines {
		if !l.HasDuration || l.Duration < policy.SlowRequest {
			continue
		}
		key := MessageShape(l.Message)
		s, ok := shapes[key]
		if !ok {
			s = &shape{perMinute: map[int64]int{}}
			shapes[key] = s
		}
		s.count++
		s.perMinute[l.At.Unix()/60]++
		if l.Duration > s.slowest.Duration {
			s.slowest = l
		}
	}
	var out []Issue
	for key, s := range shapes {
		issue := Issue{
			Kind: IssueSlowRequest, Certainty: CertaintyFact, Subject: key, LogQuery: s.slowest.Message,
			Summary: plural(s.count, "request") + " over " + policy.SlowRequest.String() + ", slowest " + s.slowest.Duration.Round(time.Millisecond).String(),
			Trend:   minuteTrend(s.perMinute, now, 10),
		}
		issue.Goroutine, issue.At, issue.HasGoroutine = locate(s.slowest)
		issue.From = s.slowest.RequestStartedAt()
		out = append(out, issue)
	}
	return out
}

// LeakIssues は検出したリークを Issue にする。代表の G は、直近の dump でその生成元から作られた待機中の G
func LeakIssues(leaks []Leak, goroutines []Goroutine, dumpAt time.Time) []Issue {
	var out []Issue
	for _, l := range leaks {
		issue := Issue{
			Kind: IssueLeak, Certainty: l.Certainty, Subject: string(l.Origin), Location: string(l.Origin),
			Summary: plural(l.Count, "goroutine") + " stuck",
			At:      dumpAt, From: dumpAt.Add(-defaultIssueWindow),
		}
		if l.Growth > 0 {
			issue.Summary += ", growing"
		}
		for _, g := range goroutines {
			if g.Origin() == l.Origin && g.StateCategory() == GoroutineWaiting {
				issue.Goroutine, issue.HasGoroutine = g.ID, true
				if g.Leaked {
					break
				}
			}
		}
		out = append(out, issue)
	}
	return out
}

// DetectRuntimeIssues は 1 つの録画の区切りから、CPU 待ちと GC の頻発を判定する。どちらも固定の閾値なので FACT
func DetectRuntimeIssues(c TraceChunk, policy IssuePolicy) []Issue {
	w := c.Timeline.Window
	if w <= 0 {
		return nil
	}
	var out []Issue
	totals := c.Timeline.TotalByCause()
	if ratio := float64(totals[CauseCPUWait]) / float64(w); policy.CPUWaitRatio > 0 && ratio >= policy.CPUWaitRatio {
		out = append(out, Issue{
			Kind: IssueCPUWaitSpike, Certainty: CertaintyFact,
			Subject: c.StartedAt.Format(time.TimeOnly),
			Summary: formatRatio(ratio) + " goroutines waiting for a P on average",
			At:      c.StartedAt.Add(w / 2), From: c.StartedAt,
		})
	}
	if perSec := float64(c.Timeline.GCCycles) / w.Seconds(); policy.GCPerSecond > 0 && perSec >= policy.GCPerSecond {
		issue := Issue{
			Kind: IssueMemoryPressure, Certainty: CertaintyFact,
			Subject: c.StartedAt.Format(time.TimeOnly),
			Summary: formatRatio(perSec) + " GC cycles per second",
			At:      c.StartedAt.Add(w / 2), From: c.StartedAt,
		}
		if g, at, ok := c.Timeline.LongestMarkAssist(); ok {
			issue.Goroutine, issue.HasGoroutine, issue.At = g, true, c.StartedAt.Add(at)
			issue.From = issue.At.Add(-defaultIssueWindow)
		}
		out = append(out, issue)
	}
	return out
}

// SortIssues は FACT を先に、その中は種類の順に並べる
func SortIssues(issues []Issue) {
	order := map[IssueKind]int{IssueLeak: 0, IssueErrorSpike: 1, IssueSlowRequest: 2, IssueCPUWaitSpike: 3, IssueMemoryPressure: 4}
	slices.SortStableFunc(issues, func(a, b Issue) int {
		if a.Certainty != b.Certainty {
			if a.Certainty == CertaintyFact {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(order[a.Kind], order[b.Kind]), cmp.Compare(a.Subject, b.Subject))
	})
}

const defaultIssueWindow = 250 * time.Millisecond

func minuteTrend(perMinute map[int64]int, now time.Time, n int) []int {
	trend := make([]int, n)
	last := now.Unix() / 60
	for i := range n {
		trend[i] = perMinute[last-int64(n-1-i)]
	}
	return trend
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

func formatRatio(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}
