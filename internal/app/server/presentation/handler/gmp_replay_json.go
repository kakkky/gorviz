package handler

import (
	"encoding/json"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// gmpReplayJSON は gmp_replay_controller.js が読む形。イベントは件数が多いので配列に詰め、文字列は表の添字で持つ:
//   - G: [atNs, 0, goroutine, place, proc (-1 = なし), thread (-1 = なし), reason (reasons の添字, -1 = なし), function (functions の添字, -1 = なし)]
//   - P: [atNs, 1, proc, status (0 = idle, 1 = running), thread (-1 = なし)]
//   - entries: [goroutine, functions の添字] の組。G ごとの入口の関数
//   - lanes: P ごとに、G が走った区間 [startNs, endNs, goroutine] を並べたもの。procs と同じ順
type gmpReplayJSON struct {
	WindowNs int64 `json:"windowNs"`
	// StartedAtNs は窓の時刻 0 に当たる Unix ナノ秒。G をクリックしたとき Goroutine 画面に時刻を渡すのに使う
	StartedAtNs int64       `json:"startedAtNs"`
	Procs       []int64     `json:"procs"`
	Lanes       [][][]int64 `json:"lanes"`
	Reasons     []string    `json:"reasons"`
	Functions   []string    `json:"functions"`
	Entries     [][]int64   `json:"entries"`
	Events      [][]int64   `json:"events"`
	Leaked      []int64     `json:"leaked"`
	Truncated   bool        `json:"truncated"`
	// Logs と Focus は Explore でログから再生するときだけ入る
	Logs  []gmpReplayLogJSON  `json:"logs"`
	Focus *gmpReplayFocusJSON `json:"focus,omitempty"`
}

type gmpReplayLogJSON struct {
	AtNs      int64  `json:"atNs"`
	Text      string `json:"text"`
	Level     string `json:"level"`
	Goroutine int64  `json:"g"`
}

type gmpReplayFocusJSON struct {
	AtNs      int64 `json:"atNs"`
	Goroutine int64 `json:"g"`
	Exact     bool  `json:"exact"`
}

var placeCodes = map[model.GoroutinePlaceKind]int64{
	model.PlaceRunning:     0,
	model.PlaceLocalQueue:  1,
	model.PlaceGlobalQueue: 2,
	model.PlaceWaiting:     3,
	model.PlaceSyscall:     4,
	model.PlaceDead:        5,
}

func newGMPReplayJSON(tl model.SchedulerTimeline, leaked []model.GoroutineID, startedAt time.Time) gmpReplayJSON {
	r := gmpReplayJSON{
		StartedAtNs: startedAt.UnixNano(),
		Logs:        []gmpReplayLogJSON{},
		WindowNs:    int64(tl.Window),
		Procs:       []int64{},
		Lanes:       [][][]int64{},
		Reasons:     []string{},
		Functions:   []string{},
		Entries:     make([][]int64, 0, len(tl.Entries)),
		Events:      make([][]int64, 0, len(tl.Events)),
		Leaked:      []int64{},
		Truncated:   tl.EventsTruncated,
	}
	for _, l := range domain.SortedProcs(tl) {
		r.Procs = append(r.Procs, int64(l.Proc))
		spans := make([][]int64, 0, len(l.Spans))
		for _, s := range l.Spans {
			spans = append(spans, []int64{int64(s.Start), int64(s.End), int64(s.Goroutine)})
		}
		r.Lanes = append(r.Lanes, spans)
	}
	reasonIdx := map[string]int64{}
	functionIdx := map[string]int64{}
	indexOf := func(fn string) int64 {
		if fn == "" {
			return -1
		}
		idx, ok := functionIdx[fn]
		if !ok {
			idx = int64(len(r.Functions))
			functionIdx[fn] = idx
			r.Functions = append(r.Functions, fn)
		}
		return idx
	}
	for g, fn := range tl.Entries {
		r.Entries = append(r.Entries, []int64{int64(g), indexOf(fn)})
	}
	for _, e := range tl.Events {
		switch e.Kind {
		case model.SchedEventGoroutine:
			proc, thread, reason := int64(-1), int64(-1), int64(-1)
			if e.Place.HasProc {
				proc = int64(e.Place.Proc)
			}
			switch e.Place.Kind {
			case model.PlaceRunning, model.PlaceSyscall:
				thread = int64(e.Place.Thread)
			case model.PlaceWaiting:
				idx, ok := reasonIdx[e.Place.Reason]
				if !ok {
					idx = int64(len(r.Reasons))
					reasonIdx[e.Place.Reason] = idx
					r.Reasons = append(r.Reasons, e.Place.Reason)
				}
				reason = idx
			}
			r.Events = append(r.Events, []int64{int64(e.At), 0, int64(e.Goroutine), placeCodes[e.Place.Kind], proc, thread, reason, indexOf(e.Function)})
		case model.SchedEventProc:
			status, thread := int64(0), int64(-1)
			if e.ProcStatus == model.ProcRunning {
				status, thread = 1, int64(e.Thread)
			}
			r.Events = append(r.Events, []int64{int64(e.At), 1, int64(e.Proc), status, thread})
		}
	}
	for _, g := range leaked {
		r.Leaked = append(r.Leaked, int64(g))
	}
	return r
}

func (r gmpReplayJSON) marshal() (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", model.ErrInternal.Wrap(err, "marshal gmp replay")
	}
	return string(b), nil
}
