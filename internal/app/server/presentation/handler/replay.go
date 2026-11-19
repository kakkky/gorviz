package handler

import (
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// replayLengths は GMP 再生で選べる長さ (Scheduler と Goroutine で共通)。録画の区切り (agent の -chunk、既定 2 秒) より長くは切り出せない
var replayLengths = []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}

// replayWindow は再生する区間の指定。After / Before は再生の続き・前 (その時刻から・その時刻までの区間) で、
// 窓の端まで再生したり 1 歩ずつ進めたりしたときに付く。At はその時刻を真ん中にした区間。どれもなければ最新
type replayWindow struct {
	At     time.Time
	HasAt  bool
	After  time.Time
	Before time.Time
	Length time.Duration
}

func (w replayWindow) targeted() bool {
	return w.HasAt || !w.After.IsZero() || !w.Before.IsZero()
}

func (w replayWindow) input() usecase.GetSchedulerReplayUsecaseInput {
	in := usecase.GetSchedulerReplayUsecaseInput{After: w.After, Before: w.Before, Length: w.Length}
	if w.HasAt {
		in.At = w.At
	}
	return in
}

// replayWindowOf は再生する区間を読む。期間を選ぶ部品の入力欄 (since / until) があれば、その範囲をそのまま (長さは最長の選べる長さまで) 再生する。
// なければ ?after= / ?before= / ?at= (Unix ナノ秒) と ?length=。長さが選べる長さでなければ fallback
func replayWindowOf(r *http.Request, fallback time.Duration) replayWindow {
	since, okSince, _ := localTimeParam(r, "since")
	until, okUntil, _ := localTimeParam(r, "until")
	if okSince && okUntil && until.After(since) {
		return replayWindow{At: since.Add(until.Sub(since) / 2), HasAt: true, Length: min(until.Sub(since), replayLengths[len(replayLengths)-1])}
	}
	w := replayWindow{Length: fallback}
	if d, err := time.ParseDuration(r.URL.Query().Get("length")); err == nil && slices.Contains(replayLengths, d) {
		w.Length = d
	}
	w.After, _ = unixNanoParam(r, "after")
	w.Before, _ = unixNanoParam(r, "before")
	if at, err := unixNanoParam(r, "at"); err == nil {
		w.At, w.HasAt = at, true
	}
	return w
}

// skippedBefore は再生の続きを開いたとき、前の窓の終わり (After) からこの窓の頭までの、録画のない時間
func skippedBefore(w replayWindow, startedAt time.Time) time.Duration {
	if w.After.IsZero() || startedAt.Sub(w.After) <= time.Millisecond {
		return 0
	}
	return startedAt.Sub(w.After)
}

// replayPicker は Scheduler・Goroutine の画面の見出しの、再生する区間を選ぶ部品。よく使う長さは選べる再生の長さで、押すと同じ真ん中のまま長さだけを替える。
// 入力欄は ms まで選べ、その範囲をそのまま再生する
func replayPicker(r *http.Request, at time.Time, hasAt bool, length time.Duration, from, to time.Time) components.TimeRangePickerViewModel {
	vm := components.TimeRangePickerViewModel{
		Range: timeRange(from, to), Action: r.URL.Path, TimeZone: timeZoneLabel(), InputStep: "0.001",
		Since: from.Local().Format(localTimeInputLayoutMs), Until: to.Local().Format(localTimeInputLayoutMs),
	}
	base := r.URL.Query()
	for _, k := range []string{"since", "until", "after", "before", "autoplay", "position"} {
		base.Del(k)
	}
	if hasAt {
		base.Set("at", strconv.FormatInt(at.UnixNano(), 10))
	}
	for _, l := range replayLengths {
		q := maps.Clone(base)
		q.Set("length", l.String())
		vm.Presets = append(vm.Presets, components.TimeRangePresetViewModel{Label: l.String(), URL: r.URL.Path + "?" + q.Encode(), Active: l == length})
	}
	if id, ok := logIDParam(r); ok {
		vm.Keep = []components.KeptParamViewModel{{Name: "log", Value: strconv.FormatInt(int64(id), 10)}}
	}
	return vm
}

// schedulerURLAt は Scheduler で、at を真ん中にした length の区間を再生する URL
func schedulerURLAt(at time.Time, length time.Duration) string {
	return "/scheduler?" + url.Values{"at": {strconv.FormatInt(at.UnixNano(), 10)}, "length": {length.String()}}.Encode()
}

// withLog は再生の URL に、注目するログ (Logs から開いたときの行) を付ける
func withLog(u string, id model.LogLineID) string {
	return u + "&" + url.Values{"log": {strconv.FormatInt(int64(id), 10)}}.Encode()
}

func logIDParam(r *http.Request) (model.LogLineID, bool) {
	n, err := strconv.ParseInt(r.URL.Query().Get("log"), 10, 64)
	return model.LogLineID(n), err == nil
}

// addReplayLogs は再生に、窓に当たるログの印を足す。focus のログが窓にあれば、その印を返す。
// ログを書いた G は、trace の印で確かに分かったときと推し量ったときとで分けて渡す (推し量った G を確かな G と同じに見せると、別の G のログに見えることがある)
func addReplayLogs(replay *gmpReplayJSON, marks []usecase.LogMark, startedAt time.Time, focus model.LogLineID, hasFocus bool) (usecase.LogMark, bool) {
	var (
		found usecase.LogMark
		ok    bool
	)
	for _, m := range marks {
		g, probably := int64(-1), int64(-1)
		switch {
		case m.Exact:
			g = int64(m.Goroutine)
		case m.Inferred:
			probably = int64(m.Goroutine)
		}
		log := gmpReplayLogJSON{AtNs: int64(m.At.Sub(startedAt)), Text: m.Line.Message, Level: string(m.Line.Level), Goroutine: g, ProbablyGoroutine: probably}
		var fields []components.LogFieldViewModel
		fields, log.Detail = logDetail(m.Line)
		for _, f := range fields {
			log.Fields = append(log.Fields, [2]string{f.Key, f.Value})
		}
		replay.Logs = append(replay.Logs, log)
		if hasFocus && m.Line.ID == focus {
			found, ok = m, true
		}
	}
	return found, ok
}

// replayRange は再生の区間。選んだ長さより短いのは録画の区切りの端で切れたときなので、そう書く (gmp_replay_controller.js の drawWindow と同じ)

