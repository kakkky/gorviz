package domain

import (
	"cmp"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
)

// MetricStep は集計の一番細かい区切り。Overview の推移はこれを束ねて描く
const MetricStep = 10 * time.Second

// 1 つの dump から集計に残す生成元の数。Top origins の推移に使う
const metricOriginsLimit = 20

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
