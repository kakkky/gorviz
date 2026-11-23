package domain

import (
	"cmp"
	"slices"
	"time"
)

type PreservedKind string

const (
	PreservedAroundIssue    PreservedKind = "around-issue"
	PreservedAroundErrorLog PreservedKind = "around-error-log"
)

func (k PreservedKind) DataKind() DataKind {
	if k == PreservedAroundIssue {
		return DataExecutionTracesAroundIssues
	}
	return DataExecutionTracesAroundErrorLogs
}

// PreservedTrace は Issue の発生・エラーログの前後に残した execution trace 1 つ。Dir は Issue のファイル名か、メッセージの形の名前
type PreservedTrace struct {
	Kind PreservedKind `json:"kind"`
	Dir  string        `json:"dir"`
	At   time.Time     `json:"at"`
	From time.Time     `json:"from"`
	To   time.Time     `json:"to"`
}

const preservedTimeFormat = "2006-01-02T15-04-05"

// FileName は <時刻>.pb.gz の時刻の部分
func (p PreservedTrace) FileName() string { return p.At.UTC().Format(preservedTimeFormat) }

func ParsePreservedTime(name string) (time.Time, bool) {
	t, err := time.Parse(preservedTimeFormat, name)
	return t, err == nil
}

// Uncovered は [from, to] のうち、同じディレクトリに前に残したものと重ならない一番長い部分。全部重なっていれば false
func Uncovered(from, to time.Time, existing []PreservedTrace) (time.Time, time.Time, bool) {
	gaps := []TimeRange{{From: from, To: to}}
	for _, e := range existing {
		var next []TimeRange
		for _, g := range gaps {
			if !e.From.Before(g.To) || !e.To.After(g.From) {
				next = append(next, g)
				continue
			}
			if e.From.After(g.From) {
				next = append(next, TimeRange{From: g.From, To: e.From})
			}
			if e.To.Before(g.To) {
				next = append(next, TimeRange{From: e.To, To: g.To})
			}
		}
		gaps = next
	}
	var best TimeRange
	for _, g := range gaps {
		if g.To.Sub(g.From) > best.To.Sub(best.From) {
			best = g
		}
	}
	return best.From, best.To, best.From.Before(best.To)
}

// Nearby は at の近く (同じ日の前後) に残した execution trace を、近い順に limit 個
func Nearby(traces []PreservedTrace, at time.Time, limit int) []PreservedTrace {
	out := slices.Clone(traces)
	slices.SortStableFunc(out, func(a, b PreservedTrace) int { return cmp.Compare(a.At.Sub(at).Abs(), b.At.Sub(at).Abs()) })
	return out[:min(len(out), limit)]
}

// RelativePath はインスタンスのディレクトリからのパス。Issue のファイルの発生の一覧に書き、そこから引けるようにする
func (p PreservedTrace) RelativePath() string {
	k := p.Kind.DataKind()
	return string(k) + "/" + k.PeriodStart(p.At).Format("2006-01-02") + "/" + p.Dir + "/" + p.FileName() + ".pb.gz"
}
