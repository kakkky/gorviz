package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// metricMaxSteps は 1 つの推移に並べる区切りの数の上限。画面の幅で見分けられるくらいにする
const metricMaxSteps = 120

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

	for i, p := range rolled {
		out.Steps = append(out.Steps, stepSummary(p))
		for o, g := range p.Origins {
			if out.Origins[o] == nil {
				out.Origins[o] = make([]MetricSample, len(rolled))
			}
			out.Origins[o][i] = sampleOf(g)
		}
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
	return s
}

func sampleOf(g domain.Gauge) MetricSample {
	v, ok := g.Mean()
	return MetricSample{Value: v, Max: g.Max, OK: ok}
}
