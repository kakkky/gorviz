package components

import (
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// GMPReplayProps は GMP の再生のアイランドの props。初期値だけを持ち、再生する窓は ReplayWindow の JSON で別に渡す。
// GoroutineURL は G を選んだときに開く Goroutine の URL で、{goroutine} を G の番号に、from・to を見ている窓に置き換えて使う。
// NextURL は見ている範囲の続きの Scheduler (最新を追っているときは空)
type GMPReplayProps struct {
	Live           bool   `json:"live"`
	FocusGoroutine int64  `json:"focusGoroutine"`
	GoroutineURL   string `json:"goroutineUrl"`
	NextURL        string `json:"nextUrl"`
	PrevURL        string `json:"prevUrl"`
	Autoplay       bool   `json:"autoplay"`
}

// ReplayWindow は再生する execution trace の 1 区間。時刻は StartedAtMs からのナノ秒。
// Functions・Reasons は関数名と待機理由の表で、イベントはその番号で引く (-1 はなし)
type ReplayWindow struct {
	From          string         `json:"from"`
	To            string         `json:"to"`
	StartedAtMs   float64        `json:"startedAtMs"`
	DurationNs    int64          `json:"durationNs"`
	Procs         []int64        `json:"procs"`
	Functions     []string       `json:"functions"`
	Reasons       []string       `json:"reasons"`
	Entries       map[string]int `json:"entries"`
	Events        []ReplayEvent  `json:"events"`
	ProcEvents    []ReplayProc   `json:"procEvents"`
	Lanes         [][]ReplaySpan `json:"lanes"`
	STW           []ReplayRange  `json:"stw"`
	GCMark        []ReplayRange  `json:"gcMark"`
	Leaks         []ReplayLeak   `json:"leaks"`
	LeakCheckAt   string         `json:"leakCheckAt"`
	FromPreserved bool           `json:"fromPreserved"`
	Truncated     bool           `json:"truncated"`
}

// ReplayEvent は G の状態の変化 1 つ。Place は ReplayPlaceKinds、Cause は domain.MainCauses の番号。Proc が -1 なら P を持たない
type ReplayEvent struct {
	At       int64 `json:"t"`
	G        int64 `json:"g"`
	Place    int   `json:"k"`
	Proc     int64 `json:"p"`
	Thread   int64 `json:"m"`
	Reason   int   `json:"r"`
	Function int   `json:"f"`
	Cause    int   `json:"c"`
	WokenBy  int64 `json:"w,omitempty"`
}

// ReplayProc は P の状態の変化 1 つ。Running でなければ Thread は -1
type ReplayProc struct {
	At      int64 `json:"t"`
	Proc    int64 `json:"p"`
	Running bool  `json:"run"`
	Thread  int64 `json:"m"`
}

// ReplaySpan は P の上で G が走った区間
type ReplaySpan struct {
	Start int64 `json:"s"`
	End   int64 `json:"e"`
	G     int64 `json:"g"`
}

// ReplayRange は STW (Reason は Reasons の番号) か GC の mark の区間
type ReplayRange struct {
	Start  int64 `json:"s"`
	End    int64 `json:"e"`
	Reason int   `json:"r"`
	ByGC   bool  `json:"gc"`
}

// ReplayLeak はリークと決めた G。同じスタックの Of 個のうち Count 個がリーク
type ReplayLeak struct {
	G         int64            `json:"g"`
	Certainty domain.Certainty `json:"certainty"`
	Count     int              `json:"count"`
	Of        int              `json:"of"`
}

// ReplayPlaceKinds は ReplayEvent.Place の番号の並び
var ReplayPlaceKinds = []observation.PlaceKind{
	observation.PlaceRunning, observation.PlaceLocalQueue, observation.PlaceGlobalQueue,
	observation.PlaceWaiting, observation.PlaceSyscall, observation.PlaceDead,
}
