package domain

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

type IssueKind string

const (
	IssueGoroutineLeak   IssueKind = "GOROUTINE_LEAK"
	IssueCPUContention   IssueKind = "CPU_CONTENTION"
	IssueGCPressure      IssueKind = "GC_PRESSURE"
	IssueLockContention  IssueKind = "LOCK_CONTENTION"
	IssueLongSTW         IssueKind = "LONG_STW"
	IssueThreadExplosion IssueKind = "THREAD_EXPLOSION"
)

var IssueKinds = []IssueKind{IssueGoroutineLeak, IssueCPUContention, IssueGCPressure, IssueLockContention, IssueLongSTW, IssueThreadExplosion}

func (k IssueKind) NextAction() string {
	switch k {
	case IssueGoroutineLeak:
		return "生成元の go 文と止まっている場所を見て、goroutine が終われる道 (context のキャンセル・channel を閉じる・タイムアウト) があるかを見直す"
	case IssueCPUContention:
		return "同じ時間帯に CPU を長く使っている goroutine の処理を見直す。または CPU の割り当て (コンテナの CPU の上限・インスタンスの数) を見直す"
	case IssueGCPressure:
		return "mark assist の長い goroutine の割り当てを減らす。または GOGC・GOMEMLIMIT を見直す"
	case IssueLockContention:
		return "待ちの多かった Lock を呼んだ関数を見て、ロックの粒度・持つ時間・分け方を見直す"
	case IssueLongSTW:
		return "STW の理由を見て、STW を起こす呼び出し (runtime.ReadMemStats・runtime.GC・プロファイルの取得など) を見直す"
	case IssueThreadExplosion:
		return "syscall・cgo の場所を見て、同時に呼ぶ数を絞る"
	}
	return ""
}

// Judge は発生を判定したやり方。同じ Issue でもやり方ごとに発生を追う
type Judge string

const (
	JudgeByThreshold  Judge = "threshold"
	JudgeByBaseline   Judge = "baseline"
	JudgeByLeakCheck  Judge = "leak-check"
	JudgeByLeakGrowth Judge = "leak-growth"
)

const (
	schedP99Threshold     = time.Millisecond
	gcShareThreshold      = 0.5
	markAssistThreshold   = 0.05
	stwThreshold          = time.Millisecond
	threadLimit           = 10000
	impactLimit           = 10
	minSchedSamples       = 20
	minLockWait           = time.Millisecond
	leakGrowthMinSamples  = 6
	leakGrowthMinIncrease = 10
)

// Impact は影響の大きかった goroutine (または同じスタック・同じ場所の goroutine のまとまり) 1 つ
type Impact struct {
	Goroutine         observation.GoroutineID   `json:"goroutine,omitempty"`
	Goroutines        []observation.GoroutineID `json:"goroutines,omitempty"`
	Entry             string                    `json:"entry,omitempty"`
	Function          string                    `json:"function,omitempty"`
	Location          string                    `json:"location,omitempty"`
	CreatedBy         string                    `json:"createdBy,omitempty"`
	CreatedByLocation string                    `json:"createdByLocation,omitempty"`
	Reason            string                    `json:"reason,omitempty"`
	Duration          time.Duration             `json:"duration,omitempty"`
	Max               time.Duration             `json:"max,omitempty"`
	Count             int                       `json:"count,omitempty"`
	Of                int                       `json:"of,omitempty"`
	Share             float64                   `json:"share,omitempty"`
	HolderGoroutine   observation.GoroutineID   `json:"holderGoroutine,omitempty"`
	HolderFunction    string                    `json:"holderFunction,omitempty"`
	HolderLocation    string                    `json:"holderLocation,omitempty"`
	Certainty         Certainty                 `json:"certainty,omitempty"`
}

// Finding は 1 回の判定で条件を満たしたこと。Sites は LOCK_CONTENTION の Lock を呼んだ場所ごとの行、GOROUTINE_LEAK のスタックごとの行
type Finding struct {
	Kind      IssueKind
	Subject   string
	Certainty Certainty
	Judge     Judge
	Value     float64
	Reason    string
	Impacts   []Impact
	Sites     []Impact
}

func IssueName(kind IssueKind, subject string) string {
	return FileName(string(kind), subject)
}

func (f Finding) IssueName() string { return IssueName(f.Kind, f.Subject) }

// JudgeChunk はチャンク 1 つを固定の線で判定する。LOCK_CONTENTION などインスタンス全体の値で判定するものは、サービスの名前を対象にする
func JudgeChunk(service observation.ServiceName, c observation.TraceChunk) []Finding {
	s := ChunkSample(c)
	var out []Finding
	if p99, ok := s.SchedP99(); ok && s.SchedLatency.N() >= minSchedSamples && p99 >= schedP99Threshold {
		out = append(out, Finding{Kind: IssueCPUContention, Subject: string(service), Certainty: CertaintyInference, Judge: JudgeByThreshold, Value: p99.Seconds(),
			Reason: fmt.Sprintf("RUNNABLE から RUNNING になるまでの p99 が %s (1ms 以上)", p99), Impacts: topByCause(c, CauseRunnable)})
	}
	if share := s.GCShare(); share >= gcShareThreshold {
		out = append(out, Finding{Kind: IssueGCPressure, Subject: string(service), Certainty: CertaintyFact, Judge: JudgeByThreshold, Value: share,
			Reason: fmt.Sprintf("GC の CPU の割合が %.1f%% (50%% 以上)", share*100), Impacts: topMarkAssist(c)})
	} else if share := s.MarkAssistShare(); share > markAssistThreshold {
		out = append(out, Finding{Kind: IssueGCPressure, Subject: string(service), Certainty: CertaintyInference, Judge: JudgeByThreshold, Value: share,
			Reason: fmt.Sprintf("mark assist の CPU の割合が %.1f%% (5%% 超)", share*100), Impacts: topMarkAssist(c)})
	}
	if s.LockWait >= minLockWait && s.LockWait > s.Running {
		out = append(out, Finding{Kind: IssueLockContention, Subject: string(service), Certainty: CertaintyInference, Judge: JudgeByThreshold, Value: s.LockWaitRatio(),
			Reason: fmt.Sprintf("ロックで待たされた時間 %s が実行中の時間 %s を超えた", s.LockWait, s.Running), Impacts: topByCause(c, CauseLockWait), Sites: lockSites(c)})
	}
	stws := map[string][]observation.GCRange{}
	for _, r := range c.GC {
		if r.Kind == observation.GCStopTheWorld && r.End-r.Start >= stwThreshold {
			stws[r.Reason] = append(stws[r.Reason], r)
		}
	}
	for _, reason := range slices.Sorted(mapKeysSeq(stws)) {
		var longest time.Duration
		var impacts []Impact
		for _, r := range stws[reason] {
			longest = max(longest, r.End-r.Start)
			impacts = append(impacts, Impact{Goroutine: r.Goroutine, Entry: c.Entries[r.Goroutine], Reason: r.Reason, Duration: r.End - r.Start})
		}
		out = append(out, Finding{Kind: IssueLongSTW, Subject: reason, Certainty: CertaintyInference, Judge: JudgeByThreshold, Value: longest.Seconds(),
			Reason: fmt.Sprintf("1 回の STW が %s (1ms 以上)", longest), Impacts: topImpacts(impacts)})
	}
	if c.Threads > threadLimit/2 {
		out = append(out, Finding{Kind: IssueThreadExplosion, Subject: string(service), Certainty: CertaintyInference, Judge: JudgeByThreshold, Value: float64(c.Threads),
			Reason: fmt.Sprintf("スレッドの数が %d (上限 %d の半分を超えた)", c.Threads, threadLimit), Impacts: append(topByCause(c, CauseIOWait), topByCause(c, CauseCgo)...)})
	}
	return out
}

func mapKeysSeq[K comparable, V any](m map[K]V) func(func(K) bool) {
	return func(yield func(K) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

func topImpacts(in []Impact) []Impact {
	slices.SortStableFunc(in, func(a, b Impact) int {
		return cmp.Or(cmp.Compare(b.Duration, a.Duration), cmp.Compare(b.Count, a.Count), cmp.Compare(a.Goroutine, b.Goroutine))
	})
	return in[:min(len(in), impactLimit)]
}

// topByCause は cause の状態にいた時間が長かった goroutine
func topByCause(c observation.TraceChunk, cause MainCause) []Impact {
	var out []Impact
	for _, g := range Goroutines(c, 0, c.Duration) {
		var total time.Duration
		var place StopPlace
		for _, s := range Stretches(c, g) {
			if s.Cause != cause {
				continue
			}
			total += s.Duration()
			if s.Duration() > place.Duration {
				place = StopPlace{Function: s.Enter.Function, Location: s.Enter.Location, WaitReason: s.Enter.Place.Reason, Duration: s.Duration()}
			}
		}
		if total > 0 {
			out = append(out, Impact{Goroutine: g, Entry: c.Entries[g], Function: place.Function, Location: place.Location, Reason: place.WaitReason, Duration: total, Max: place.Duration})
		}
	}
	return topImpacts(out)
}

func topMarkAssist(c observation.TraceChunk) []Impact {
	per := map[observation.GoroutineID]time.Duration{}
	for _, r := range c.GC {
		if r.Kind == observation.GCMarkAssist {
			per[r.Goroutine] += r.End - r.Start
		}
	}
	var out []Impact
	for g, d := range per {
		out = append(out, Impact{Goroutine: g, Entry: c.Entries[g], Duration: d})
	}
	return topImpacts(out)
}

// lockSites は Lock を呼んだ場所ごとに、待ち時間の割合・回数・平均と最大と、待ちを終わらせた (ロックを持っていた) 側
func lockSites(c observation.TraceChunk) []Impact {
	holders := map[string]Impact{}
	for _, g := range Goroutines(c, 0, c.Duration) {
		for _, s := range Stretches(c, g) {
			if s.Cause != CauseLockWait || !s.HasLeave || s.Leave.WokenBy == 0 {
				continue
			}
			if _, ok := holders[s.Enter.Location]; !ok {
				holders[s.Enter.Location] = Impact{HolderGoroutine: s.Leave.WokenBy, HolderFunction: s.Leave.WokenByFunction, HolderLocation: s.Leave.WokenByLocation}
			}
		}
	}
	bySite := map[string]*Impact{}
	var total time.Duration
	for _, w := range c.Stats.Waits {
		if w.Kind != observation.PlaceWaiting || !IsLockFunction(w.Function) {
			continue
		}
		total += w.Total
		s, ok := bySite[w.Location]
		if !ok {
			h := holders[w.Location]
			s = &Impact{Function: w.Function, Location: w.Location, Entry: w.Entry, HolderGoroutine: h.HolderGoroutine, HolderFunction: h.HolderFunction, HolderLocation: h.HolderLocation}
			bySite[w.Location] = s
		}
		s.Count += w.Count
		s.Duration += w.Total
		s.Max = max(s.Max, w.Max)
	}
	out := make([]Impact, 0, len(bySite))
	for _, s := range bySite {
		s.Share = ratio(s.Duration, total)
		out = append(out, *s)
	}
	return topImpacts(out)
}

// stackKey は runtime の内部を除いたスタック。goroutineleak プロファイルと trace のスタックで、runtime の内側の深さが違うことがあるため
func stackKey(stack []observation.Frame) string {
	var b strings.Builder
	for _, f := range stack {
		if strings.HasPrefix(f.Function, "runtime.") {
			continue
		}
		b.WriteString(f.Function)
		b.WriteByte('@')
		b.WriteString(f.Location())
		b.WriteByte('\n')
	}
	return b.String()
}

// JudgeByLeakCheck は runtime がリークと判定したスタックを、入口の関数ごとの Issue にする。snapshot は同じ時点の goroutine で、件数の M と goroutine を引くのに使う
func JudgeLeakCheck(lc observation.GoroutineLeakCheck, snapshot observation.GoroutineSnapshot, hasSnapshot bool) []Finding {
	if lc.Status != observation.LeakCheckDone {
		return nil
	}
	var matches []LeakMatch
	if hasSnapshot {
		matches = MatchLeaks(lc, snapshot)
	}
	bySubject := map[string]*Finding{}
	var subjects []string
	for i, l := range lc.Leaks {
		if l.Count <= 0 {
			continue
		}
		subject := NormalizeFunction(observation.EntryFunction(l.Stack))
		f, ok := bySubject[subject]
		if !ok {
			f = &Finding{Kind: IssueGoroutineLeak, Subject: subject, Certainty: CertaintyFact, Judge: JudgeByLeakCheck}
			bySubject[subject] = f
			subjects = append(subjects, subject)
		}
		cur := observation.CurrentFrame(l.Stack)
		site := Impact{Entry: observation.EntryFunction(l.Stack), Function: cur.Function, Location: cur.Location(), Count: l.Count, Of: l.Count}
		if i < len(matches) {
			m := matches[i]
			site.Of, site.Goroutines, site.Certainty = m.Of, m.Goroutines[:min(len(m.Goroutines), impactLimit)], m.Certainty
			if m.CreatedBy.Function != "" {
				site.CreatedBy, site.CreatedByLocation = m.CreatedBy.Function, m.CreatedBy.Location()
			}
		}
		f.Value += float64(l.Count)
		f.Sites = append(f.Sites, site)
	}
	out := make([]Finding, 0, len(subjects))
	for _, s := range subjects {
		f := bySubject[s]
		f.Reason = fmt.Sprintf("runtime の goroutineleak プロファイルが %d 個のリークを判定した", int(f.Value))
		f.Sites = topImpacts(f.Sites)
		f.Impacts = f.Sites
		out = append(out, *f)
	}
	return out
}

// creatorOf は生成元の関数。分からなければ入口で代用する
func creatorOf(g observation.Goroutine) string {
	if g.CreatedBy.Function != "" {
		return NormalizeFunction(g.CreatedBy.Function)
	}
	return NormalizeFunction(g.Entry)
}

// JudgeByLeakGrowth は goroutineleak プロファイルのない古い Go 向けに、生成元ごとの待機中の数が減らずに増え続けているかを見る。
// snapshots は時刻順で、1 分ごとに 1 つずつ間引いて比べる
func JudgeLeakGrowth(snapshots []observation.GoroutineSnapshot) []Finding {
	var samples []observation.GoroutineSnapshot
	for _, s := range snapshots {
		if n := len(samples); n > 0 && s.At.Truncate(time.Minute).Equal(samples[n-1].At.Truncate(time.Minute)) {
			samples[n-1] = s
			continue
		}
		samples = append(samples, s)
	}
	if len(samples) < leakGrowthMinSamples {
		return nil
	}
	counts := make([]map[string]int, len(samples))
	for i, s := range samples {
		counts[i] = map[string]int{}
		for _, g := range s.Goroutines {
			if g.State == observation.GoroutineWaiting {
				counts[i][creatorOf(g)]++
			}
		}
	}
	last := counts[len(counts)-1]
	var out []Finding
	for _, creator := range slices.Sorted(mapKeysSeq(last)) {
		growing := true
		for i := 1; i < len(counts) && growing; i++ {
			growing = counts[i][creator] >= counts[i-1][creator]
		}
		if !growing || last[creator]-counts[0][creator] < leakGrowthMinIncrease {
			continue
		}
		f := Finding{Kind: IssueGoroutineLeak, Subject: creator, Certainty: CertaintyInference, Judge: JudgeByLeakGrowth, Value: float64(last[creator]),
			Reason: fmt.Sprintf("待機中の goroutine が %d 分で %d 個から %d 個へ減らずに増えた", len(samples), counts[0][creator], last[creator])}
		sites := map[string]*Impact{}
		for _, g := range samples[len(samples)-1].Goroutines {
			if g.State != observation.GoroutineWaiting || creatorOf(g) != creator {
				continue
			}
			k := g.Current.Location()
			s, ok := sites[k]
			if !ok {
				s = &Impact{Entry: g.Entry, Function: g.Current.Function, Location: k, CreatedBy: g.CreatedBy.Function, CreatedByLocation: g.CreatedBy.Location(), Reason: g.WaitReason, Certainty: CertaintyInference}
				sites[k] = s
			}
			s.Count++
			s.Of++
			if len(s.Goroutines) < impactLimit {
				s.Goroutines = append(s.Goroutines, g.ID)
			}
		}
		for _, s := range sites {
			f.Sites = append(f.Sites, *s)
		}
		f.Sites = topImpacts(f.Sites)
		f.Impacts = f.Sites
		out = append(out, f)
	}
	return out
}
