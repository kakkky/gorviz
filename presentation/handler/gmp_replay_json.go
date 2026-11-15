package handler

import (
	"encoding/json"
	"time"

	"github.com/kakkky/gorviz/domain"
)

// gmpReplayJSON は gmp_replay_controller.js が読む形。イベントは件数が多いので配列に詰め、文字列は表の添字で持つ:
//   - G: [atNs, 0, goroutine, place, proc (-1 = なし), thread (-1 = なし), reason (reasons の添字, -1 = なし), function (functions の添字, -1 = なし)]
//   - P: [atNs, 1, proc, status (0 = idle, 1 = running), thread (-1 = なし)]
//   - entries: [goroutine, functions の添字] の組。G ごとの入口の関数
type gmpReplayJSON struct {
	WindowNs int64 `json:"windowNs"`
	// StartedAtNs は窓の時刻 0 に当たる Unix ナノ秒。G をクリックしたとき Goroutine 画面に時刻を渡すのに使う
	StartedAtNs int64     `json:"startedAtNs"`
	Procs       []int64   `json:"procs"`
	Reasons     []string  `json:"reasons"`
	Functions   []string  `json:"functions"`
	Entries     [][]int64 `json:"entries"`
	Events      [][]int64 `json:"events"`
	Leaked      []int64   `json:"leaked"`
	Truncated   bool      `json:"truncated"`
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

var placeCodes = map[domain.GoroutinePlaceKind]int64{
	domain.PlaceRunning:     0,
	domain.PlaceLocalQueue:  1,
	domain.PlaceGlobalQueue: 2,
	domain.PlaceWaiting:     3,
	domain.PlaceSyscall:     4,
	domain.PlaceDead:        5,
}

func newGMPReplayJSON(tl domain.SchedulerTimeline, leaked []domain.GoroutineID, startedAt time.Time) gmpReplayJSON {
	r := gmpReplayJSON{
		StartedAtNs: startedAt.UnixNano(),
		Logs:        []gmpReplayLogJSON{},
		WindowNs:    int64(tl.Window),
		Procs:       []int64{},
		Reasons:     []string{},
		Functions:   []string{},
		Entries:     make([][]int64, 0, len(tl.Entries)),
		Events:      make([][]int64, 0, len(tl.Events)),
		Leaked:      []int64{},
		Truncated:   tl.EventsTruncated,
	}
	for _, l := range tl.SortedProcs() {
		r.Procs = append(r.Procs, int64(l.Proc))
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
		case domain.SchedEventGoroutine:
			proc, thread, reason := int64(-1), int64(-1), int64(-1)
			if e.Place.HasProc {
				proc = int64(e.Place.Proc)
			}
			switch e.Place.Kind {
			case domain.PlaceRunning, domain.PlaceSyscall:
				thread = int64(e.Place.Thread)
			case domain.PlaceWaiting:
				idx, ok := reasonIdx[e.Place.Reason]
				if !ok {
					idx = int64(len(r.Reasons))
					reasonIdx[e.Place.Reason] = idx
					r.Reasons = append(r.Reasons, e.Place.Reason)
				}
				reason = idx
			}
			r.Events = append(r.Events, []int64{int64(e.At), 0, int64(e.Goroutine), placeCodes[e.Place.Kind], proc, thread, reason, indexOf(e.Function)})
		case domain.SchedEventProc:
			status, thread := int64(0), int64(-1)
			if e.ProcStatus == domain.ProcRunning {
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
		return "", domain.ErrInternal.Wrap(err, "marshal gmp replay")
	}
	return string(b), nil
}
