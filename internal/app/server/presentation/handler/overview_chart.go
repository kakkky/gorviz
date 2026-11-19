package handler

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// グラフは viewBox の座標で描き、横に引き伸ばす。線の太さは vector-effect で引き伸ばしの影響を受けない
const (
	chartWidth  = 1000.0
	chartHeight = 160.0
	chartXTicks = 5
)

// chartYIntervals は縦軸の目盛りの間の数の目安。値の範囲をこのくらいに分け、間隔を 1・2・5 × 10^n に丸める
const chartYIntervals = 5

// chartSeries は 1 本の線 (か積み上げの 1 段) の、区切りごとの値
type chartSeries struct {
	label  string
	class  string
	values []usecase.MetricSample
}

// chartFrame は推移のグラフどうしで共通の横軸。すべてのグラフを同じ期間・同じ区切りで描く
type chartFrame struct {
	steps  []time.Time
	step   time.Duration
	span   time.Duration
	noData string
}

func newChartFrame(m usecase.GetMetricsUsecaseOutput, span time.Duration) chartFrame {
	f := chartFrame{step: m.Step, span: span}
	for _, s := range m.Steps {
		f.steps = append(f.steps, s.At)
	}
	if len(f.steps) == 0 {
		return f
	}
	from, to := f.steps[0], f.steps[len(f.steps)-1].Add(m.Step)
	switch {
	case !m.HasSince:
		f.noData = "100%"
	case m.Since.After(from):
		f.noData = fmt.Sprintf("%.2f%%", min(float64(m.Since.Sub(from))/float64(to.Sub(from)), 1)*100)
	}
	return f
}

// stepsJSON は全グラフで共通の横軸。hover で出す時刻と、ドラッグで拡大するときの時刻に JS が使う
func (f chartFrame) stepsJSON() string {
	type stepsJSON struct {
		StepMillis int64    `json:"stepMillis"`
		Times      []int64  `json:"times"`
		Labels     []string `json:"labels"`
		Step       string   `json:"step"`
	}
	out := stepsJSON{StepMillis: f.step.Milliseconds(), Step: formatStep(f.step)}
	for _, t := range f.steps {
		out.Times = append(out.Times, t.UnixMilli())
		out.Labels = append(out.Labels, f.tooltipLabel(t))
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// tooltipLabel は hover で出す時刻。目盛りより細かく、日をまたぐ期間なら日付も付ける
func (f chartFrame) tooltipLabel(t time.Time) string {
	if f.span > 24*time.Hour {
		return t.Format("01/02 15:04:05")
	}
	return t.Format(time.TimeOnly)
}

func (f chartFrame) x(i int) float64 {
	return (float64(i) + 0.5) / float64(len(f.steps)) * chartWidth
}

func (f chartFrame) label(t time.Time) string {
	switch {
	case f.span > 7*24*time.Hour:
		return t.Format("01/02")
	case f.span > 24*time.Hour:
		return t.Format("01/02 15:04")
	case f.step < time.Minute:
		return t.Format(time.TimeOnly)
	default:
		return t.Format("15:04")
	}
}

// base は目盛りと補助線を置いた、系列のないグラフ
func (f chartFrame) base(scale yScale, format func(float64) string) components.ChartViewModel {
	vm := components.ChartViewModel{NoData: f.noData}
	for k := range scale.ticks() {
		v := scale.lo + float64(k)*scale.step
		y := scale.y(v)
		text := "0"
		// 所要時間やバイト数の書式は 0 を「値なし」(-) として書くので、0 の目盛りだけは数字で書く
		if v != 0 {
			text = format(v)
		}
		vm.YTicks = append(vm.YTicks, components.ChartTickViewModel{Pos: fmt.Sprintf("%.2f%%", (chartHeight-y)/chartHeight*100), Text: text})
		if k > 0 {
			vm.GridY = append(vm.GridY, num(y))
		}
	}
	n := len(f.steps)
	for k := range chartXTicks {
		if n == 0 {
			break
		}
		i := k * (n - 1) / (chartXTicks - 1)
		vm.XLabels = append(vm.XLabels, components.ChartTickViewModel{Pos: fmt.Sprintf("%.2f%%", f.x(i)/chartWidth*100), Text: f.label(f.steps[i])})
		vm.GridX = append(vm.GridX, num(f.x(i)))
	}
	return vm
}

// chartReadout は hover で出す、系列ごと・区切りごとの値の文字列と、その点 (棒なら段の上端) の縦の位置 (viewBox の座標)。
// 値のない区切りは空と null にする。JS (chart_sync_controller) は、カーソルが線か棒の上にあるときだけ値を出す
type chartReadout struct {
	Label string     `json:"label"`
	Class string     `json:"class"`
	Text  []string   `json:"text"`
	Y     []*float64 `json:"y"`
}

func newChartReadout(s chartSeries, steps int) chartReadout {
	return chartReadout{Label: s.label, Class: s.class, Text: make([]string, steps), Y: make([]*float64, steps)}
}

func readoutJSON(rs []chartReadout) string {
	b, err := json.Marshal(rs)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// lineChart は系列ごとに、測った区切りだけを結ぶ線を描く。縦軸は表示している期間の値の最小〜最大に合わせ、拡大すると細かな動きが見えるようにする。
// threshold が 0 より大きければ、縦軸の範囲に入るときだけ Issue の閾値の線を引き、入らなければ凡例でそう書く
// unit は縦軸の目盛りを丸める単位 (バイト数なら MiB)
func (f chartFrame) lineChart(series []chartSeries, format func(float64) string, threshold, unit float64) components.ChartViewModel {
	lo, hi, seen := 0.0, 0.0, false
	for _, s := range series {
		for _, v := range s.values {
			if !v.OK {
				continue
			}
			if !seen {
				lo, hi, seen = v.Value, v.Value, true
			}
			lo, hi = min(lo, v.Value), max(hi, v.Value)
		}
	}
	scale := niceRange(lo, hi, unit)
	vm := f.base(scale, format)
	vm.Kind = "line"
	if threshold > 0 {
		vm.ThresholdLabel = "issue at " + format(threshold)
		if scale.contains(threshold) {
			vm.ThresholdY = num(scale.y(threshold))
		} else {
			vm.ThresholdLabel += " (off scale)"
		}
	}
	for _, s := range series {
		var d strings.Builder
		for i, v := range s.values {
			if !v.OK {
				continue
			}
			cmd := "L"
			if d.Len() == 0 {
				cmd = "M"
			}
			fmt.Fprintf(&d, "%s%s %s ", cmd, num(f.x(i)), num(scale.y(v.Value)))
		}
		vm.Lines = append(vm.Lines, components.ChartPathViewModel{Class: s.class, D: d.String()})
		vm.Legend = append(vm.Legend, components.ChartLegendViewModel{Label: s.label, Class: s.class})
	}
	vm.Empty = !anyOK(series)
	readouts := make([]chartReadout, 0, len(series))
	for _, s := range series {
		ro := newChartReadout(s, len(f.steps))
		for i, v := range s.values {
			if v.OK {
				y := scale.y(v.Value)
				ro.Text[i], ro.Y[i] = format(v.Value), &y
			}
		}
		readouts = append(readouts, ro)
	}
	vm.Readout = readoutJSON(readouts)
	return vm
}

// stackedBarChart は区切りごとの量を、系列を下から順に積み上げた棒で描く。棒の間と段の間に隙間をあける。format は目盛りと hover の値の書き方
func (f chartFrame) stackedBarChart(series []chartSeries, format func(float64) string) components.ChartViewModel {
	peak := 0.0
	for i := range f.steps {
		total := 0.0
		for _, s := range series {
			total += s.values[i].Value
		}
		peak = max(peak, total)
	}
	// 棒は長さで量を比べるので、縦軸は 0 から
	scale := niceRange(0, peak, 1)
	vm := f.base(scale, format)
	vm.Kind = "bar"
	slot := chartWidth / float64(len(f.steps))
	gap := min(2, slot/4)
	for i := range f.steps {
		below := 0.0
		for _, s := range series {
			v := s.values[i].Value
			if v == 0 {
				continue
			}
			y0, y1 := scale.y(below), scale.y(below+v)
			vm.Bars = append(vm.Bars, components.ChartRectViewModel{
				Class: s.class, X: num(float64(i)*slot + gap/2), W: num(slot - gap),
				Y: num(y1), H: num(max(y0-y1-1, 0.5)),
			})
			below += v
		}
	}
	for _, s := range series {
		vm.Legend = append(vm.Legend, components.ChartLegendViewModel{Label: s.label, Class: s.class})
	}
	vm.Empty = peak == 0
	// 積み上げは上の段から読むので、hover の値も上の段から並べる。縦の位置は段の上端
	readouts := make([]chartReadout, len(series))
	for k, s := range series {
		readouts[len(series)-1-k] = newChartReadout(s, len(f.steps))
	}
	for i := range f.steps {
		below := 0.0
		for k, s := range series {
			if v := s.values[i].Value; v > 0 {
				below += v
				y := scale.y(below)
				ro := &readouts[len(series)-1-k]
				ro.Text[i], ro.Y[i] = format(v), &y
			}
		}
	}
	vm.Readout = readoutJSON(readouts)
	return vm
}

// yScale は縦軸の範囲 [lo, hi] と、目盛りの間隔 step
type yScale struct {
	lo, hi, step float64
}

// y は値を viewBox の縦の位置にする。上に 4 だけ余白をあけ、線が上端で切れないようにする
func (s yScale) y(v float64) float64 {
	if s.hi <= s.lo {
		return chartHeight
	}
	return chartHeight - (v-s.lo)/(s.hi-s.lo)*(chartHeight-4)
}

func (s yScale) contains(v float64) bool {
	return s.lo <= v && v <= s.hi
}

// ticks は下端から上端までの目盛りの数
func (s yScale) ticks() int {
	if s.step <= 0 {
		return 1
	}
	return int(math.Round((s.hi-s.lo)/s.step)) + 1
}

// niceRange は [lo, hi] を含み、目盛りの間隔が unit × 1・2・5 × 10^n になる範囲。間隔は範囲をおよそ chartYIntervals 等分する長さに丸める。
// 値がすべて 0 以上なら下端は 0 未満にしない。値が 1 つしかなければ、その前後に少し幅を持たせる
func niceRange(lo, hi, unit float64) yScale {
	lo, hi = lo/unit, hi/unit
	if hi <= lo {
		if hi == 0 {
			hi = 1
		} else {
			pad := math.Abs(hi) * 0.1
			lo, hi = lo-pad, hi+pad
		}
	}
	step := niceCeil((hi - lo) / chartYIntervals)
	// 割り算の誤差で、ちょうど目盛りの上にある値が 1 つ外へ出ないようにする
	const eps = 1e-9
	nlo := math.Floor(lo/step+eps) * step
	if lo >= 0 && nlo < 0 {
		nlo = 0
	}
	nhi := math.Ceil(hi/step-eps) * step
	if nhi <= nlo {
		nhi = nlo + step
	}
	return yScale{lo: nlo * unit, hi: nhi * unit, step: step * unit}
}

func anyOK(series []chartSeries) bool {
	for _, s := range series {
		for _, v := range s.values {
			if v.OK {
				return true
			}
		}
	}
	return false
}

// niceCeil は v 以上で、1・2・5 × 10^n のいちばん小さい値。縦軸の上端に使う
func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 5, 10} {
		if v <= m*p {
			return m * p
		}
	}
	return 10 * p
}

func num(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func formatCount(v float64) string {
	if v == math.Trunc(v) {
		return strconv.Itoa(int(v))
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// formatStep は区切りの長さを "10s"・"5m"・"2h" のように短く書く
func formatStep(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return fmt.Sprintf("%ds", d/time.Second)
	}
}
