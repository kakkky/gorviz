package web

import (
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// ongoingWindow は発生が「今も続いている」とみなす直近の長さ
const ongoingWindow = 2 * domain.BucketSize

func certaintyNote(c domain.Certainty) string {
	if c == domain.CertaintyFact {
		return "said by the runtime or recorded in the execution trace"
	}
	return "inferred by gorviz from a threshold, a trend or a stack"
}

func ongoing(st usecase.GetSessionStatusUsecaseOutput, lastAt time.Duration) bool {
	return st.Session.App.State == domain.AppRunning && lastAt >= st.Elapsed-ongoingWindow
}

// GetIssuesHandler は Issue の一覧を、種類ごとの発生の数と推移と一緒に描く
type GetIssuesHandler struct {
	status *usecase.GetSessionStatusUsecase
	list   *usecase.ListIssuesUsecase
}

func NewGetIssuesHandler(status *usecase.GetSessionStatusUsecase, list *usecase.ListIssuesUsecase) *GetIssuesHandler {
	return &GetIssuesHandler{status: status, list: list}
}

func (h *GetIssuesHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	st, err := h.status.Exec(r.Context(), usecase.GetSessionStatusUsecaseInput{})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	q := r.URL.Query()
	tr, err := parseRange(q, st.Elapsed, fromSession)
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	kind := domain.IssueKind(strings.ToUpper(q.Get("kind")))
	out, err := h.list.Exec(r.Context(), usecase.ListIssuesUsecaseInput{Kind: kind, Text: q.Get("q")})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	keep := url.Values{}
	for _, k := range []string{"q", "kind"} {
		if v := q.Get(k); v != "" {
			keep.Set(k, v)
		}
	}
	vm := pages.IssuesViewModel{Base: baseOf("Issues", tabIssues, st), Picker: picker("/issues", keep, tr, phasesOf(st.Timeline, st.Elapsed)), Filtered: len(keep) > 0,
		Search: components.SearchFormViewModel{Action: "/issues", Query: q.Get("q"), Placeholder: "function", Keep: rangeKeep(tr),
			Selects: []components.SearchSelectViewModel{{Name: "kind", All: "all kinds", Options: kindOptions(), Selected: string(kind)}}}}
	var inRange []usecase.IssueSummary
	for _, is := range out.Issues {
		if n := countIn(is.Starts, tr.From, tr.To); n > 0 || is.LastAt >= tr.From && is.FirstAt <= tr.To {
			inRange = append(inRange, is)
		}
	}
	vm.Total, vm.Kinds, vm.Chart = issueTrend(inRange, tr)
	for _, is := range inRange {
		vm.Rows = append(vm.Rows, pages.IssueRowViewModel{Kind: string(is.Kind), Certainty: string(is.Certainty), CertaintyNote: certaintyNote(is.Certainty), Subject: is.Target, Summary: is.Value,
			Ongoing: ongoing(st, is.LastAt), Count: countIn(is.Starts, tr.From, tr.To), Trend: trendBars(is.Starts, tr.From, tr.To), FirstSeen: at(is.FirstAt), LastSeen: at(is.LastAt), URL: issueURL(is.ID, 0)})
	}
	render(rw, r, pages.Issues(vm))
}

func rangeKeep(tr timeRange) []components.KeptParamViewModel {
	out := []components.KeptParamViewModel{{Name: "from", Value: tr.FromRaw}}
	if tr.ToRaw != "" {
		out = append(out, components.KeptParamViewModel{Name: "to", Value: tr.ToRaw})
	}
	return out
}

func kindOptions() []components.SearchOptionViewModel {
	out := make([]components.SearchOptionViewModel, 0, len(domain.IssueKinds))
	for _, k := range domain.IssueKinds {
		out = append(out, components.SearchOptionViewModel{Value: string(k), Label: string(k)})
	}
	return out
}

func countIn(starts []time.Duration, from, to time.Duration) int {
	n := 0
	for _, s := range starts {
		if s >= from && s <= to {
			n++
		}
	}
	return n
}

const trendBuckets = 24

// trendBars は期間を区切り、区切りごとの発生の数を、いちばん多い区切りに対する高さにする
func trendBars(starts []time.Duration, from, to time.Duration) []string {
	counts := make([]int, trendBuckets)
	span := to - from
	top := 0
	for _, t := range starts {
		if span <= 0 || t < from || t > to {
			continue
		}
		i := min(int(float64(trendBuckets)*float64(t-from)/float64(span)), trendBuckets-1)
		counts[i]++
		top = max(top, counts[i])
	}
	out := make([]string, trendBuckets)
	for i, c := range counts {
		out[i] = "0%"
		if top > 0 {
			out[i] = itoa(c*100/top) + "%"
		}
	}
	return out
}

// issueTrend は種類ごとの発生の数と、区切りごとに種類で積み上げた発生の推移
func issueTrend(issues []usecase.IssueSummary, tr timeRange) (int, []pages.IssueKindTotalViewModel, components.ChartViewModel) {
	const buckets = 48
	span := max(tr.To-tr.From, time.Second)
	step := max(span/buckets, time.Second)
	n := int((span + step - 1) / step)
	times := make([]time.Duration, n)
	for i := range times {
		times[i] = tr.From + step*time.Duration(i)
	}
	byKind := map[domain.IssueKind][]float64{}
	totals := map[domain.IssueKind]int{}
	total := 0
	for _, is := range issues {
		v, ok := byKind[is.Kind]
		if !ok {
			v = make([]float64, n)
			byKind[is.Kind] = v
		}
		for _, t := range is.Starts {
			if t < tr.From || t > tr.To {
				continue
			}
			v[min(int((t-tr.From)/step), n-1)]++
			totals[is.Kind]++
			total++
		}
	}
	var kinds []pages.IssueKindTotalViewModel
	var ss []series
	for i, k := range domain.IssueKinds {
		class := "series-" + itoa(i%6+1)
		if totals[k] > 0 {
			kinds = append(kinds, pages.IssueKindTotalViewModel{Kind: string(k), Count: totals[k], Percent: itoa(totals[k]*100/total) + "%", Class: class})
		}
		if v, ok := byKind[k]; ok {
			ss = append(ss, series{label: string(k), class: class, bar: true, stack: true, values: v})
		}
	}
	return total, kinds, buildChart(chartSpec{from: tr.From, to: tr.From + span, step: step, times: times, series: ss, format: count, threshold: math.NaN(), zoom: true})
}
