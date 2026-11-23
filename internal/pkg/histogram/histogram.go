package histogram

import (
	"math/bits"
	"time"
)

const buckets = 25

// Histogram の i 番目の区切りは (2^(i-1), 2^i] µs で、最後は残り全部。2 倍ずつにしてあるので、足し合わせても分位点を区切りの粗さで出せる
type Histogram struct {
	Counts []uint64      `json:"counts,omitempty"`
	Max    time.Duration `json:"max,omitempty"`
}

func (h *Histogram) Observe(d time.Duration) {
	if h.Counts == nil {
		h.Counts = make([]uint64, buckets)
	}
	h.Counts[bucketOf(d)]++
	h.Max = max(h.Max, d)
}

func (h *Histogram) Merge(o Histogram) {
	if len(o.Counts) == 0 {
		return
	}
	if h.Counts == nil {
		h.Counts = make([]uint64, buckets)
	}
	for i, c := range o.Counts {
		if i < buckets {
			h.Counts[i] += c
		}
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

// Quantile は区切りの上端で答えるので、実際より最大 2 倍まで大きく出る (Max は超えない)
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
	return min(bits.Len64(us-1), buckets-1)
}

func bucketUpper(i int) time.Duration {
	if i == buckets-1 {
		return time.Duration(1<<62 - 1)
	}
	return time.Duration(uint64(1)<<i) * time.Microsecond
}
