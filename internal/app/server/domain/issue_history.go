package domain

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
)

const (
	// IssueRecordInterval より短い間隔では、同じ Issue を積み直さない。判定は refresh ごとに走るので、続いている Issue を間引く
	IssueRecordInterval = time.Minute
	// IssueActiveWindow の中に見つかった Issue を、今も起きている (active) とみなす
	IssueActiveWindow = 2 * time.Minute
	// IssueRetention は Issue と、その前後に保存した録画を持つ期間。Overview の粗い集計と同じく 30 日強
	IssueRetention = 31 * 24 * time.Hour
)

// IssueOccurrence は Issue が見つかった 1 回。SeenAt は見つけた時刻で、At / From / Goroutine はそのときの調べに入る先
type IssueOccurrence struct {
	Fingerprint  string            `json:"fingerprint"`
	Kind         model.IssueKind   `json:"kind"`
	Certainty    Certainty         `json:"certainty"`
	Subject      string            `json:"subject"`
	Summary      string            `json:"summary"`
	Location     string            `json:"location,omitempty"`
	Goroutine    model.GoroutineID `json:"goroutine,omitempty"`
	HasGoroutine bool              `json:"hasGoroutine,omitempty"`
	SeenAt       time.Time         `json:"seenAt"`
	At           time.Time         `json:"at"`
	From         time.Time         `json:"from"`
}

// IssueFingerprint は同じ Issue とみなす単位。リークは生成元ごと、CPU 待ちと GC の頻発はプロセスで 1 つ
// (時間帯ごとに別の Issue にすると、同じ問題の推移を追えない)
func IssueFingerprint(i Issue) string {
	if i.Kind == model.IssueLeak {
		return string(i.Kind) + ":" + i.Subject
	}
	return string(i.Kind)
}

func NewIssueOccurrence(i Issue, seenAt time.Time) IssueOccurrence {
	return IssueOccurrence{
		Fingerprint: IssueFingerprint(i), Kind: i.Kind, Certainty: i.Certainty, Subject: i.Subject, Summary: i.Summary, Location: i.Location,
		Goroutine: i.Goroutine, HasGoroutine: i.HasGoroutine, SeenAt: seenAt, At: i.At, From: i.From,
	}
}

// IssueSummary は期間の中の 1 つの Issue。Latest はいちばん新しく見つかった 1 回で、表示する説明や調べに入る先はこれを使う
type IssueSummary struct {
	Latest   IssueOccurrence
	Count    int
	LastSeen time.Time
	Active   bool
	// Trend は期間を等分した区切りごとの、見つかった回数
	Trend []int
}

// SummarizeIssues は [from, to] の occurrences を Issue ごとにまとめる。今も起きているものを先に、その中は最後に見つかった順に並べる
func SummarizeIssues(occurrences []IssueOccurrence, from, to, now time.Time, buckets int) []IssueSummary {
	byFingerprint := map[string]*IssueSummary{}
	span := to.Sub(from)
	for _, o := range occurrences {
		if o.SeenAt.Before(from) || o.SeenAt.After(to) {
			continue
		}
		s, ok := byFingerprint[o.Fingerprint]
		if !ok {
			s = &IssueSummary{Trend: make([]int, buckets)}
			byFingerprint[o.Fingerprint] = s
		}
		s.Count++
		if !o.SeenAt.Before(s.LastSeen) {
			s.Latest, s.LastSeen = o, o.SeenAt
		}
		if buckets > 0 && span > 0 {
			s.Trend[min(int(int64(o.SeenAt.Sub(from))*int64(buckets)/int64(span)), buckets-1)]++
		}
	}
	out := make([]IssueSummary, 0, len(byFingerprint))
	for _, s := range byFingerprint {
		s.Active = now.Sub(s.LastSeen) <= IssueActiveWindow
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b IssueSummary) int {
		if a.Active != b.Active {
			if a.Active {
				return -1
			}
			return 1
		}
		return cmp.Or(b.LastSeen.Compare(a.LastSeen), strings.Compare(a.Latest.Fingerprint, b.Latest.Fingerprint))
	})
	return out
}

// MatchesIssueQuery は、生成元・説明・場所のどれかに query (大文字小文字を区別しない) を含むか。空なら何でも合う
func MatchesIssueQuery(o IssueOccurrence, query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	for _, s := range []string{o.Subject, o.Summary, o.Location, string(o.Kind)} {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}
