package web

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
)

// series はグラフの系列 1 つ。値のない区切りは NaN
type series struct {
	label  string
	class  string
	values []float64
	dashed bool
	bar    bool
	// stack は棒を前の系列の上に積む
	stack bool
}

type chartMark struct {
	at    time.Time
	class string
	title string
}

// chartSpec は区切り (times) ごとの値の時系列グラフ。format は値の書き方、threshold は判定の線 (なければ NaN)
type chartSpec struct {
	title     string
	note      string
	from, to  time.Time
	step      time.Duration
	times     []time.Time
	series    []series
	format    func(float64) string
	threshold float64
	thLabel   string
	marks     []chartMark
	hoverURL  func(i int) string
}

func coord(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

// niceMax は目盛りの切りのよい上限 (1・2・5 × 10^n)
func niceMax(v float64) float64 {
	if v <= 0 || math.IsNaN(v) {
		return 1
	}
	mag := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*mag >= v {
			return m * mag
		}
	}
	return 10 * mag
}

const (
	plotWidth  = 1000.0
	plotHeight = 160.0
)

func pct(v float64) string { return strconv.FormatFloat(v*100, 'f', 2, 64) + "%" }

func buildChart(c chartSpec) components.ChartViewModel {
	vm := components.ChartViewModel{Title: c.title, Note: c.note}
	span := c.to.Sub(c.from)
	if span <= 0 {
		span = time.Second
	}
	frac := func(t time.Time) float64 { return float64(t.Sub(c.from)) / float64(span) }
	x := func(t time.Time) float64 { return plotWidth * frac(t) }
	top := 0.0
	any := false
	stacked := make([]float64, len(c.times))
	for _, s := range c.series {
		for i, v := range s.values {
			if math.IsNaN(v) {
				continue
			}
			any = true
			if s.stack {
				stacked[i] += v
				top = math.Max(top, stacked[i])
			} else {
				top = math.Max(top, v)
			}
		}
	}
	if !math.IsNaN(c.threshold) && c.threshold > 0 && top > c.threshold/4 {
		top = math.Max(top, c.threshold)
	}
	top = niceMax(top * 1.05)
	y := func(v float64) float64 { return plotHeight - plotHeight*math.Min(v, top)/top }
	for i := 0; i <= 4; i++ {
		v := top * float64(i) / 4
		vm.YTicks = append(vm.YTicks, components.ChartTickViewModel{Pos: pct(float64(i) / 4), Label: c.format(v)})
		if i > 0 {
			vm.GridY = append(vm.GridY, coord(y(v)))
		}
	}
	for i := 0; i <= 4; i++ {
		t := c.from.Add(span * time.Duration(i) / 4)
		label := clock(t)
		if span > 48*time.Hour {
			label = t.Local().Format("01/02 15:04")
		} else if span > 2*time.Hour {
			label = t.Local().Format("15:04")
		}
		vm.XTicks = append(vm.XTicks, components.ChartTickViewModel{Pos: pct(float64(i) / 4), Label: label})
		if i > 0 && i < 4 {
			vm.GridX = append(vm.GridX, coord(x(t)))
		}
	}
	if !math.IsNaN(c.threshold) && c.threshold > 0 && c.threshold <= top {
		vm.ThresholdY, vm.ThresholdLabel = coord(y(c.threshold)), c.thLabel
	}
	barW := plotWidth * float64(c.step) / float64(span)
	base := make([]float64, len(c.times))
	for _, s := range c.series {
		if s.bar {
			for i, v := range s.values {
				if math.IsNaN(v) || v <= 0 {
					continue
				}
				bottom := 0.0
				if s.stack {
					bottom = base[i]
					base[i] += v
				}
				vm.Bars = append(vm.Bars, components.ChartBarViewModel{Class: s.class, X: coord(x(c.times[i]) + 0.5), Y: coord(y(bottom + v)), W: coord(math.Max(barW-1, 0.8)), H: coord(y(bottom) - y(bottom+v))})
			}
		} else {
			var b strings.Builder
			pen := false
			for i, v := range s.values {
				if math.IsNaN(v) {
					pen = false
					continue
				}
				cmd := "L"
				if !pen {
					cmd = "M"
				}
				b.WriteString(cmd + coord(x(c.times[i].Add(c.step/2))) + " " + coord(y(v)) + " ")
				pen = true
			}
			vm.Lines = append(vm.Lines, components.ChartLineViewModel{Class: s.class, Path: strings.TrimSpace(b.String()), Dashed: s.dashed})
		}
		last := math.NaN()
		for i := len(s.values) - 1; i >= 0; i-- {
			if !math.IsNaN(s.values[i]) {
				last = s.values[i]
				break
			}
		}
		value := ""
		if !math.IsNaN(last) {
			value = c.format(last)
		}
		vm.Legend = append(vm.Legend, components.ChartLegendViewModel{Label: s.label, Class: s.class, Value: value})
	}
	for _, m := range c.marks {
		if m.at.Before(c.from) || m.at.After(c.to) {
			continue
		}
		vm.Marks = append(vm.Marks, components.ChartMarkViewModel{X: coord(x(m.at)), Class: m.class, Title: m.title})
	}
	for i, t := range c.times {
		var title strings.Builder
		title.WriteString(span2(t, c.step))
		for _, s := range c.series {
			v := "-"
			if !math.IsNaN(s.values[i]) {
				v = c.format(s.values[i])
			}
			title.WriteString("\n" + s.label + ": " + v)
		}
		h := components.ChartHoverViewModel{X: coord(x(t)), W: coord(math.Max(barW, 0.8)), Title: title.String()}
		if c.hoverURL != nil {
			h.URL = c.hoverURL(i)
		}
		vm.Hovers = append(vm.Hovers, h)
	}
	if !any {
		vm.Empty = "no data in this period"
	}
	return vm
}

func span2(t time.Time, step time.Duration) string {
	return dateTime(t) + " – " + clock(t.Add(step))
}
