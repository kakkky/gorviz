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

func buildChart(c chartSpec) components.ChartViewModel {
	vm := components.ChartViewModel{Title: c.title, Note: c.note}
	span := c.to.Sub(c.from)
	if span <= 0 {
		span = time.Second
	}
	width := float64(components.ChartRight - components.ChartLeft)
	height := float64(components.ChartBottom - components.ChartTop)
	x := func(t time.Time) float64 {
		return float64(components.ChartLeft) + width*float64(t.Sub(c.from))/float64(span)
	}
	top := 0.0
	any := false
	for _, s := range c.series {
		for _, v := range s.values {
			if !math.IsNaN(v) {
				top, any = math.Max(top, v), true
			}
		}
	}
	if !math.IsNaN(c.threshold) && c.threshold > 0 && top > c.threshold/4 {
		top = math.Max(top, c.threshold)
	}
	top = niceMax(top * 1.05)
	y := func(v float64) float64 { return float64(components.ChartBottom) - height*math.Min(v, top)/top }
	for _, v := range []float64{0, top / 2, top} {
		vm.YTicks = append(vm.YTicks, components.ChartTickViewModel{Pos: coord(y(v)), Label: c.format(v)})
	}
	for i := 0; i <= 4; i++ {
		t := c.from.Add(span * time.Duration(i) / 4)
		label := clock(t)
		if span > 48*time.Hour {
			label = t.Local().Format("01/02 15:04")
		} else if span > 2*time.Hour {
			label = t.Local().Format("15:04")
		}
		vm.XTicks = append(vm.XTicks, components.ChartTickViewModel{Pos: coord(math.Min(math.Max(x(t), components.ChartLeft+24), components.ChartRight-24)), Label: label})
	}
	if !math.IsNaN(c.threshold) && c.threshold > 0 && c.threshold <= top {
		vm.Threshold = &components.ChartTickViewModel{Pos: coord(y(c.threshold)), Label: c.thLabel}
	}
	barW := width * float64(c.step) / float64(span)
	for _, s := range c.series {
		last := math.NaN()
		if s.bar {
			for i, v := range s.values {
				if math.IsNaN(v) || v <= 0 {
					continue
				}
				vm.Bars = append(vm.Bars, components.ChartBarViewModel{Class: s.class, X: coord(x(c.times[i])), Y: coord(y(v)), W: coord(math.Max(barW-0.5, 0.6)), H: coord(float64(components.ChartBottom) - y(v))})
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
		for i := len(s.values) - 1; i >= 0; i-- {
			if !math.IsNaN(s.values[i]) {
				last = s.values[i]
				break
			}
		}
		value := "-"
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
		h := components.ChartHoverViewModel{X: coord(x(t)), W: coord(math.Max(barW, 0.6)), Title: title.String()}
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
