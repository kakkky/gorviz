package domain

import (
	"math/bits"
	"time"
)

const histogramSubBins = 8

// Histogram は時間の分布。2 のべき乗の区間をさらに 8 つに分けて数える。足し合わせられるので、時間幅ごとに持って期間で合わせる
type Histogram struct {
	Counts [64 * histogramSubBins]uint32
	Total  uint64
}

func histogramBin(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	v := uint64(d)
	n := bits.Len64(v)
	if n <= 3 {
		return int(v)
	}
	sub := (v >> (n - 4)) & (histogramSubBins - 1)
	return n*histogramSubBins + int(sub)
}

// binUpper は区間の上の端。分位を区間の上の端で答えるので、閾値の判定は大きめに出る側に倒れる
func binUpper(i int) time.Duration {
	if i < histogramSubBins {
		return time.Duration(i)
	}
	n := i / histogramSubBins
	sub := uint64(i % histogramSubBins)
	return time.Duration((histogramSubBins + sub + 1) << (n - 4))
}

func (h *Histogram) Add(d time.Duration) {
	i := histogramBin(d)
	if i >= len(h.Counts) {
		i = len(h.Counts) - 1
	}
	h.Counts[i]++
	h.Total++
}

func (h *Histogram) Merge(o *Histogram) {
	if o.Total == 0 {
		return
	}
	for i, c := range o.Counts {
		h.Counts[i] += c
	}
	h.Total += o.Total
}

func (h *Histogram) Quantile(q float64) time.Duration {
	if h.Total == 0 {
		return 0
	}
	want := uint64(float64(h.Total)*q + 0.5)
	if want == 0 {
		want = 1
	}
	var seen uint64
	for i, c := range h.Counts {
		seen += uint64(c)
		if seen >= want {
			return binUpper(i)
		}
	}
	return binUpper(len(h.Counts) - 1)
}
