package domain

import (
	"cmp"
	"fmt"
	"math"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// Issue は gorviz が検出した、続いている傾向 1 件。
// Goroutine / At は調べに入る先の代表的な G とその時点 (なければ HasGoroutine が false)。
// From は Goroutine 画面で見る区間の始まり
type Issue struct {
	Kind      model.IssueKind
	Certainty Certainty
	// Subject は 1 件の単位 (生成元・時間帯など) を表す文字列
	Subject string
	Summary string
	// Location は関係する場所 (file:line)
	Location     string
	Trend        []int
	Goroutine    model.GoroutineID
	HasGoroutine bool
	At           time.Time
	From         time.Time
}

type IssuePolicy struct {
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

// MessageShape はメッセージの数値や ID を伏せた形。録画の ID など、同じ種類のものを 1 つの名前にまとめるのに使う
func MessageShape(msg string) string {
	msg = uuidRe.ReplaceAllString(msg, "<id>")
	msg = hexRe.ReplaceAllString(msg, "<hex>")
	return numberRe.ReplaceAllString(msg, "<n>")
}

// LeakIssues は検出したリークを Issue にする。代表の G は、直近の dump でその生成元から作られた待機中の G
func LeakIssues(leaks []Leak, goroutines []model.Goroutine, dumpAt time.Time) []Issue {
	var out []Issue
	for _, l := range leaks {
		issue := Issue{
			Kind: model.IssueLeak, Certainty: l.Certainty, Subject: string(l.Origin), Location: string(l.Origin),
			Summary: plural(l.Count, "goroutine") + " stuck",
			Trend:   l.Trend,
			At:      dumpAt, From: dumpAt.Add(-defaultIssueWindow),
		}
		if l.Growth > 0 {
			issue.Summary += ", growing"
		}
		for _, g := range goroutines {
			if g.Origin() == l.Origin && g.StateCategory() == model.GoroutineWaiting {
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
// ProcessSubject は、プロセス全体で 1 つの Issue (CPU 待ち・GC の頻発) の Subject。いつ起きたかは見つけた時刻で追う
const ProcessSubject = "process"

func DetectRuntimeIssues(c model.TraceChunk, policy IssuePolicy) []Issue {
	w := c.Timeline.Window
	if w <= 0 {
		return nil
	}
	var out []Issue
	// P を待っていた量は再生用イベントの打ち切りに左右されない集計 (Stats) から測る
	if s := c.Stats; s.Window > 0 && policy.CPUWaitRatio > 0 {
		if ratio := float64(s.Runnable) / float64(s.Window); ratio >= policy.CPUWaitRatio {
			issue := Issue{
				Kind: model.IssueCPUWaitSpike, Certainty: CertaintyFact,
				Subject: ProcessSubject,
				Summary: formatRatio(ratio) + " goroutines waiting for a P on average",
				At:      c.StartedAt.Add(w / 2), From: c.StartedAt,
			}
			if p99, ok := s.SchedLatency.Quantile(0.99); ok {
				issue.Summary += ", p99 wait to run " + FormatShortDuration(p99)
			}
			out = append(out, issue)
		}
	}
	if perSec := float64(c.Timeline.GCCycles) / w.Seconds(); policy.GCPerSecond > 0 && perSec >= policy.GCPerSecond {
		issue := Issue{
			Kind: model.IssueMemoryPressure, Certainty: CertaintyFact,
			Subject: ProcessSubject,
			Summary: formatRatio(perSec) + " GC cycles per second",
			At:      c.StartedAt.Add(w / 2), From: c.StartedAt,
		}
		if share := c.Stats.GCMaxShare; share > 0 {
			issue.Summary += fmt.Sprintf(", up to %d%% of the CPU taken by GC in %s", int(share*100+0.5), FormatShortDuration(model.MMUWindow))
		}
		if g, at, ok := LongestMarkAssist(c.Timeline); ok {
			issue.Goroutine, issue.HasGoroutine, issue.At = g, true, c.StartedAt.Add(at)
			issue.From = issue.At.Add(-defaultIssueWindow)
		}
		out = append(out, issue)
	}
	return out
}

// SortIssues は FACT を先に、その中は種類の順に並べる
func SortIssues(issues []Issue) {
	order := map[model.IssueKind]int{model.IssueLeak: 0, model.IssueCPUWaitSpike: 1, model.IssueMemoryPressure: 2}
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

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

// FormatShortDuration は短い時間を "38ms"・"250µs"・"1.2s" のように有効数字 2 桁で書く (990ms は 990ms のまま、指数にしない)
func FormatShortDuration(d time.Duration) string {
	switch {
	case d >= time.Second:
		return twoDigits(d.Seconds()) + "s"
	case d >= time.Millisecond:
		return twoDigits(float64(d)/float64(time.Millisecond)) + "ms"
	default:
		return twoDigits(float64(d)/float64(time.Microsecond)) + "µs"
	}
}

func twoDigits(v float64) string {
	if v <= 0 {
		return "0"
	}
	unit := math.Pow(10, math.Floor(math.Log10(v))-1)
	return strconv.FormatFloat(math.Round(v/unit)*unit, 'f', max(-int(math.Floor(math.Log10(unit))), 0), 64)
}

func formatRatio(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}
