package domain

import (
	"cmp"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// MetricStep は集計の一番細かい区切り。Overview の推移はこれを束ねて描く
const MetricStep = 10 * time.Second

// 1 つの dump から集計に残す生成元の数。Top origins の推移に使う
const metricOriginsLimit = 20

// 1 つの録画の区切りから集計に残す待ち場所の数。待った時間の長い順
const metricWaitsLimit = 20

// MetricPoint は MetricStep (かそれを束ねた長さ) 1 つ分の集計。どの値も足し合わせられる形で持つので、
// 同じ区切りに何度か足すことも、細かい区切りを粗い区切りへ束ねることもできる
type MetricPoint struct {
	At time.Time `json:"at"`
	// States と Origins は dump から測る。dump がなかった区切りでは空
	States      map[model.GoroutineStateCategory]Gauge `json:"states,omitempty"`
	Origins     map[model.GoroutineOrigin]Gauge        `json:"origins,omitempty"`
	HeapObjects Gauge                                  `json:"heapObjects"`
	HeapGoal    Gauge                                  `json:"heapGoal"`
	// LiveHeap と EffectiveGOGC は GC が終わるたびに測る (model.GCEnd)。GC がなかった区切りでは空
	LiveHeap      Gauge `json:"liveHeap"`
	EffectiveGOGC Gauge `json:"effectiveGOGC"`
	// MarkAssist は 1 秒あたりに G が GC を手伝わされた時間 (秒)。アプリの G がその分止められている
	MarkAssist  Gauge `json:"markAssist"`
	GCPerSecond Gauge `json:"gcPerSecond"`
	CPUWaiting  Gauge `json:"cpuWaiting"`
	// SchedLatency は G が runnable になってから走り出すまでの時間の分布、STW は GC の stop-the-world 1 回の長さの分布
	SchedLatency model.Histogram `json:"schedLatency"`
	STW          model.Histogram `json:"stw"`
	// GCMaxShare は録画の区切りごとの、model.MMUWindow の窓で GC が P の時間を取った割合の最大。1 - Max が最悪の MMU
	GCMaxShare Gauge `json:"gcMaxShare"`
	// ProcRunning と ProcGC は全 P の時間のうち、G を走らせていた割合と GC に使われた割合。残りは P が空いていた
	ProcRunning Gauge `json:"procRunning"`
	ProcGC      Gauge `json:"procGC"`
	// Waits は待ち場所ごとの待った量。録画の区切りごとに、待った時間の長い順に metricWaitsLimit まで足す。
	// TracedSeconds は Waits を数えた録画の長さの合計 (秒)。待った時間をこれで割ると、そこで同時に待っていた G の数の平均になる
	Waits         map[WaitSiteKey]WaitTotal `json:"waits,omitempty"`
	TracedSeconds float64                   `json:"tracedSeconds,omitempty"`
}

// Gauge は区切りの中で何度か測った値。平均と最大を、区切りどうしを足し合わせても崩れない形で持つ
type Gauge struct {
	Sum float64 `json:"sum"`
	N   int     `json:"n"`
	Max float64 `json:"max"`
}

func (g *Gauge) Observe(v float64) {
	g.Merge(Gauge{Sum: v, N: 1, Max: v})
}

func (g *Gauge) Merge(o Gauge) {
	if o.N == 0 {
		return
	}
	if g.N == 0 || o.Max > g.Max {
		g.Max = o.Max
	}
	g.Sum += o.Sum
	g.N += o.N
}

func (g Gauge) Mean() (float64, bool) {
	if g.N == 0 {
		return 0, false
	}
	return g.Sum / float64(g.N), true
}

func (p *MetricPoint) Merge(o MetricPoint) {
	for k, g := range o.States {
		p.States = mergeGauge(p.States, k, g)
	}
	for k, g := range o.Origins {
		p.Origins = mergeGauge(p.Origins, k, g)
	}
	p.HeapObjects.Merge(o.HeapObjects)
	p.HeapGoal.Merge(o.HeapGoal)
	p.LiveHeap.Merge(o.LiveHeap)
	p.EffectiveGOGC.Merge(o.EffectiveGOGC)
	p.MarkAssist.Merge(o.MarkAssist)
	p.GCPerSecond.Merge(o.GCPerSecond)
	p.CPUWaiting.Merge(o.CPUWaiting)
	p.SchedLatency.Merge(o.SchedLatency)
	p.STW.Merge(o.STW)
	p.GCMaxShare.Merge(o.GCMaxShare)
	p.ProcRunning.Merge(o.ProcRunning)
	p.ProcGC.Merge(o.ProcGC)
	p.TracedSeconds += o.TracedSeconds
	for k, w := range o.Waits {
		if p.Waits == nil {
			p.Waits = map[WaitSiteKey]WaitTotal{}
		}
		v := p.Waits[k]
		v.Merge(w)
		p.Waits[k] = v
	}
}

// ObserveTrace は録画の区切り 1 つの集計 (待ち場所・走り出すまでの時間・GC・P の使い方) を足す
func (p *MetricPoint) ObserveTrace(s model.TraceStats) {
	p.SchedLatency.Merge(s.SchedLatency)
	p.STW.Merge(s.STW)
	if s.Window <= 0 || s.Procs == 0 {
		return
	}
	p.GCMaxShare.Observe(s.GCMaxShare)
	p.TracedSeconds += s.Window.Seconds()
	capacity := float64(s.Window) * float64(s.Procs)
	p.ProcRunning.Observe(min(float64(s.ProcRunning)/capacity, 1))
	p.ProcGC.Observe(min(float64(s.ProcGC)/capacity, 1))
	waits := map[WaitSiteKey]WaitTotal{}
	for _, w := range s.Waits {
		k := WaitSiteKey{Cause: CauseOfWaitSite(w), Entry: w.Entry, Function: w.Function, Location: w.Location}
		v := waits[k]
		v.Merge(WaitTotal{Seconds: w.Total.Seconds(), Count: w.Count, MaxSeconds: w.Max.Seconds(), Reason: w.Reason})
		waits[k] = v
	}
	for _, k := range TopWaits(waits, metricWaitsLimit) {
		p.Merge(MetricPoint{Waits: map[WaitSiteKey]WaitTotal{k: waits[k]}})
	}
}

// TopWaits は待った時間の長い順に limit 個までの待ち場所
func TopWaits(waits map[WaitSiteKey]WaitTotal, limit int) []WaitSiteKey {
	keys := make([]WaitSiteKey, 0, len(waits))
	for k := range waits {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b WaitSiteKey) int {
		return cmp.Or(cmp.Compare(waits[b].Seconds, waits[a].Seconds), cmp.Compare(a.Location, b.Location), cmp.Compare(a.Entry, b.Entry), cmp.Compare(a.Cause, b.Cause))
	})
	return keys[:min(limit, len(keys))]
}

func mergeGauge[K comparable](m map[K]Gauge, k K, g Gauge) map[K]Gauge {
	if m == nil {
		m = map[K]Gauge{}
	}
	v := m[k]
	v.Merge(g)
	m[k] = v
	return m
}

// Clone は map を共有しない複製を返す
func (p MetricPoint) Clone() MetricPoint {
	out := MetricPoint{At: p.At}
	out.Merge(p)
	return out
}

// GoroutineTotal は区切りの中の dump で数えた G の数の平均。dump がなければ false
func (p MetricPoint) GoroutineTotal() (float64, bool) {
	total, ok := 0.0, false
	for _, g := range p.States {
		if m, has := g.Mean(); has {
			total, ok = total+m, true
		}
	}
	return total, ok
}

// ObserveGoroutines は dump 1 回分の G の数を、状態ごとと生成元ごと (多い順に metricOriginsLimit まで) に足す
func (p *MetricPoint) ObserveGoroutines(states GoroutineStateCounts, gs []model.Goroutine) {
	for _, s := range []model.GoroutineStateCategory{model.GoroutineRunning, model.GoroutineRunnable, model.GoroutineSyscall, model.GoroutineWaiting} {
		p.States = mergeGauge(p.States, s, Gauge{Sum: float64(states[s]), N: 1, Max: float64(states[s])})
	}
	counts := map[model.GoroutineOrigin]int{}
	for _, g := range gs {
		counts[g.Origin()]++
	}
	origins := make([]model.GoroutineOrigin, 0, len(counts))
	for o := range counts {
		origins = append(origins, o)
	}
	slices.SortFunc(origins, func(a, b model.GoroutineOrigin) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
	})
	for _, o := range origins[:min(metricOriginsLimit, len(origins))] {
		p.Origins = mergeGauge(p.Origins, o, Gauge{Sum: float64(counts[o]), N: 1, Max: float64(counts[o])})
	}
}

// metricSteps は画面に出す区切りの長さの候補。どれも MetricStep の倍数で、5 分以上のものは 5 分の倍数
var metricSteps = []time.Duration{
	10 * time.Second, 20 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute,
	5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 6 * time.Hour,
}

// MetricStepFor は span を maxPoints 個以下の区切りで描けるいちばん細かい区切りの長さ
func MetricStepFor(span time.Duration, maxPoints int) time.Duration {
	for _, s := range metricSteps {
		if span/s <= time.Duration(maxPoints) {
			return s
		}
	}
	return metricSteps[len(metricSteps)-1]
}

// Rollup は points を step ごとに束ね、to を含む区切りで終わる span/step 個の区切りにして返す。データのない区切りも、At だけを入れて返す
func Rollup(points []MetricPoint, to time.Time, span, step time.Duration) []MetricPoint {
	n := int(span / step)
	end := to.Truncate(step).Add(step)
	start := end.Add(-time.Duration(n) * step)
	out := make([]MetricPoint, n)
	for i := range out {
		out[i].At = start.Add(time.Duration(i) * step)
	}
	for _, p := range points {
		if p.At.Before(start) || !p.At.Before(end) {
			continue
		}
		out[int(p.At.Sub(start)/step)].Merge(p)
	}
	return out
}
