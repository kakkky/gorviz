package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// metricMaxSteps は 1 つの推移に並べる区切りの数の上限。画面の幅で見分けられるくらいにする
const metricMaxSteps = 120

// metricWaitsLimit は期間全体で多かった待ち場所を返す数
const metricWaitsLimit = 10

type GetMetricsUsecase struct {
	metricStore domain.MetricStore
}

func (us *UseCase) NewGetMetricsUsecase() *GetMetricsUsecase {
	return &GetMetricsUsecase{metricStore: us.metricStore}
}

// GetMetricsUsecaseInput の Span は Now から遡って見る長さ
type GetMetricsUsecaseInput struct {
	Now  time.Time
	Span time.Duration
}

// GetMetricsUsecaseOutput は [From, To] を Step ごとに区切った推移
type GetMetricsUsecaseOutput struct {
	From  time.Time
	To    time.Time
	Step  time.Duration
	Steps []MetricStepSummary
	// Since は持っているいちばん古い集計。それより前の区切りにはデータがない
	Since    time.Time
	HasSince bool
	// Origins は生成元ごとの、区切りごとの G の数
	Origins map[model.GoroutineOrigin][]MetricSample
	// Waits は期間全体で待った時間の長い待ち場所。Traced (待ち場所を数えた録画の長さ) で割ると、そこで同時に待っていた G の数の平均になる
	Waits  []MetricWait
	Traced time.Duration
}

type MetricWait struct {
	Site  domain.WaitSiteKey
	Total domain.WaitTotal
}

type MetricStepSummary struct {
	At            time.Time
	States        map[model.GoroutineStateCategory]MetricSample
	Goroutines    MetricSample
	HeapObjects   MetricSample
	LiveHeap      MetricSample
	EffectiveGOGC MetricSample
	MarkAssist    MetricSample
	HeapGoal      MetricSample
	GCPerSecond   MetricSample
	CPUWaiting    MetricSample
	// SchedLatency は G が runnable になってから走り出すまでの時間 (秒) の分位点と最大
	SchedLatencyP50 MetricSample
	SchedLatencyP99 MetricSample
	SchedLatencyMax MetricSample
	// STW は GC の stop-the-world 1 回の長さ (秒) の分位点と最大
	STWP99 MetricSample
	STWMax MetricSample
	// MMU は区切りの中で最悪の、model.MMUWindow の窓で GC が残した CPU の割合
	MMU MetricSample
	// ProcRunning・ProcGC・ProcIdle は全 P の時間のうち、G を走らせていた・GC に使われた・空いていた割合の平均
	ProcRunning MetricSample
	ProcGC      MetricSample
	ProcIdle    MetricSample
}

// MetricSample は区切りの中の平均 (Value) と最大 (Max)。OK が false なら、その区切りでは測っていない
type MetricSample struct {
	Value float64
	Max   float64
	OK    bool
}

func (us *GetMetricsUsecase) Exec(_ context.Context, in GetMetricsUsecaseInput) (GetMetricsUsecaseOutput, error) {
	step := domain.MetricStepFor(in.Span, metricMaxSteps)
	rolled := domain.Rollup(us.metricStore.Range(in.Now.Add(-in.Span), in.Now, step), in.Now, in.Span, step)
	out := GetMetricsUsecaseOutput{Step: step, To: in.Now, Origins: map[model.GoroutineOrigin][]MetricSample{}}
	if len(rolled) > 0 {
		out.From = rolled[0].At
	}
	out.Since, out.HasSince = us.metricStore.Since()

	waits := map[domain.WaitSiteKey]domain.WaitTotal{}
	for i, p := range rolled {
		out.Steps = append(out.Steps, stepSummary(p))
		for k, w := range p.Waits {
			v := waits[k]
			v.Merge(w)
			waits[k] = v
		}
		out.Traced += time.Duration(p.TracedSeconds * float64(time.Second))
		for o, g := range p.Origins {
			if out.Origins[o] == nil {
				out.Origins[o] = make([]MetricSample, len(rolled))
			}
			out.Origins[o][i] = sampleOf(g)
		}
	}
	for _, k := range domain.TopWaits(waits, metricWaitsLimit) {
		out.Waits = append(out.Waits, MetricWait{Site: k, Total: waits[k]})
	}
	return out, nil
}

func stepSummary(p domain.MetricPoint) MetricStepSummary {
	s := MetricStepSummary{
		At: p.At, States: map[model.GoroutineStateCategory]MetricSample{},
		HeapObjects: sampleOf(p.HeapObjects), LiveHeap: sampleOf(p.LiveHeap), EffectiveGOGC: sampleOf(p.EffectiveGOGC), MarkAssist: sampleOf(p.MarkAssist), HeapGoal: sampleOf(p.HeapGoal), GCPerSecond: sampleOf(p.GCPerSecond), CPUWaiting: sampleOf(p.CPUWaiting),
	}
	for c, g := range p.States {
		s.States[c] = sampleOf(g)
	}
	s.Goroutines.Value, s.Goroutines.OK = p.GoroutineTotal()
	s.SchedLatencyP50, s.SchedLatencyP99, s.SchedLatencyMax = quantileOf(p.SchedLatency, 0.5), quantileOf(p.SchedLatency, 0.99), histogramMaxOf(p.SchedLatency)
	s.STWP99, s.STWMax = quantileOf(p.STW, 0.99), histogramMaxOf(p.STW)
	if p.GCMaxShare.N > 0 {
		s.MMU = MetricSample{Value: 1 - p.GCMaxShare.Max, Max: 1 - p.GCMaxShare.Max, OK: true}
	}
	s.ProcRunning, s.ProcGC = sampleOf(p.ProcRunning), sampleOf(p.ProcGC)
	if s.ProcRunning.OK && s.ProcGC.OK {
		idle := max(1-s.ProcRunning.Value-s.ProcGC.Value, 0)
		s.ProcIdle = MetricSample{Value: idle, Max: idle, OK: true}
	}
	return s
}

func quantileOf(h model.Histogram, q float64) MetricSample {
	d, ok := h.Quantile(q)
	return MetricSample{Value: d.Seconds(), Max: d.Seconds(), OK: ok}
}

func histogramMaxOf(h model.Histogram) MetricSample {
	if h.N() == 0 {
		return MetricSample{}
	}
	return MetricSample{Value: h.Max.Seconds(), Max: h.Max.Seconds(), OK: true}
}

func sampleOf(g domain.Gauge) MetricSample {
	v, ok := g.Mean()
	return MetricSample{Value: v, Max: g.Max, OK: ok}
}
