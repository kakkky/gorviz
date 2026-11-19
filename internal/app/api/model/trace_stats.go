package model

import (
	"math/bits"
	"time"
)

// TraceStats は録画の区切りの全イベントから数えた集計。再生用イベント (SchedulerTimeline.Events) の打ち切りに左右されない。
// FlightRecorder から切り出した区切りは前の区切りと重なるので、重なっていない部分 (Window) だけを数える
type TraceStats struct {
	Window time.Duration
	// Procs は窓の中に現れた P の数 (GOMAXPROCS)
	Procs int
	// Runnable は全 G が runnable (P を待っていた) だった時間の合計
	Runnable time.Duration
	// Waits は G が waiting・syscall にいた時間を、入ったときの場所ごとにまとめたもの
	Waits []WaitSite
	// SchedLatency は G が runnable になってから走り出すまでの時間の分布
	SchedLatency Histogram
	// STW は GC による stop-the-world 1 回の長さの分布
	STW Histogram
	// GCMaxShare は MMUWindow の窓で、GC が P の時間を取った割合の最大。1 - GCMaxShare が MMU (最小のミューテーター使用率)
	GCMaxShare float64
	// ProcRunning は全 P で G を走らせていた時間の合計 (GC のワーカーを除く)。ProcGC は GC のワーカーと STW に使われた時間の合計
	ProcRunning time.Duration
	ProcGC      time.Duration
}

// MMUWindow は GCMaxShare を測る窓の長さ。リクエスト 1 つ分くらいの間に、GC にどれだけ止められうるかを見る
const MMUWindow = 10 * time.Millisecond

// WaitSite は待ち場所 1 つ。Function は止まった関数 (sync.(*Mutex).Lock など)、Location はそれを呼んだ利用者のコードの位置。
// Total は G ごとの待った時間の合計 (同時に 10 個の G が 1 秒待てば 10 秒)。Count は窓の中で待ちに入った回数で、窓より前から続いている待ちは数えない。
// Max は 1 回の待ちのうち窓の中で見えた長さの最大
type WaitSite struct {
	Kind     GoroutinePlaceKind
	Reason   string
	Cgo      bool
	Entry    string
	Function string
	Location string
	Count    int
	Total    time.Duration
	Max      time.Duration
}

// histogramBuckets は Histogram の区切りの数。i 番目は (2^(i-1), 2^i] µs で、最後は残り全部
const histogramBuckets = 25

// Histogram は時間の分布。区切りを 2 倍ずつにしたので、足し合わせても分位点を (区切りの粗さで) 出せる
type Histogram struct {
	Counts []uint64      `json:"counts,omitempty"`
	Max    time.Duration `json:"max,omitempty"`
}

func (h *Histogram) Observe(d time.Duration) {
	if h.Counts == nil {
		h.Counts = make([]uint64, histogramBuckets)
	}
	h.Counts[bucketOf(d)]++
	h.Max = max(h.Max, d)
}

func (h *Histogram) Merge(o Histogram) {
	if len(o.Counts) == 0 {
		return
	}
	if h.Counts == nil {
		h.Counts = make([]uint64, histogramBuckets)
	}
	for i, c := range o.Counts {
		h.Counts[i] += c
	}
	h.Max = max(h.Max, o.Max)
}

func (h Histogram) N() uint64 {
	var n uint64
	for _, c := range h.Counts {
		n += c
	}
	return n
}

// Quantile は q (0〜1) の分位点。区切りの上端で答えるので、実際より最大 2 倍まで大きく出る (Max は超えない)
func (h Histogram) Quantile(q float64) (time.Duration, bool) {
	n := h.N()
	if n == 0 {
		return 0, false
	}
	rank := uint64(q*float64(n-1)) + 1
	var seen uint64
	for i, c := range h.Counts {
		seen += c
		if seen >= rank {
			return min(bucketUpper(i), h.Max), true
		}
	}
	return h.Max, true
}

func bucketOf(d time.Duration) int {
	us := uint64(max(d, 0) / time.Microsecond)
	if us == 0 {
		return 0
	}
	return min(bits.Len64(us-1), histogramBuckets-1)
}

func bucketUpper(i int) time.Duration {
	if i == histogramBuckets-1 {
		return time.Duration(1<<62 - 1)
	}
	return time.Duration(uint64(1)<<i) * time.Microsecond
}
