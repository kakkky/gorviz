package domain_test

import (
	"testing"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/stretchr/testify/assert"
)

func TestHistogramQuantile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []time.Duration
		q      float64
		lo, hi time.Duration
	}{
		{name: "returns 0 for no values", q: 0.99},
		{name: "answers within an eighth of the value", values: []time.Duration{time.Millisecond}, q: 0.5, lo: time.Millisecond, hi: time.Millisecond * 9 / 8},
		{name: "puts p99 on the slow tail", values: append(make([]time.Duration, 98), 10*time.Millisecond, 10*time.Millisecond), q: 0.99, lo: 10 * time.Millisecond, hi: 12 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var h domain.Histogram
			for _, v := range tt.values {
				h.Add(v)
			}
			got := h.Quantile(tt.q)
			assert.GreaterOrEqual(t, got, tt.lo)
			assert.LessOrEqual(t, got, tt.hi)
		})
	}
}
