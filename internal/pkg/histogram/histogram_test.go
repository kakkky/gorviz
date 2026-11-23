package histogram_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/pkg/histogram"
)

func TestHistogram_Quantile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []time.Duration
		merge  []time.Duration
		q      float64
		want   time.Duration
		wantOK bool
	}{
		{name: "answers nothing when empty", q: 0.99},
		{name: "answers the upper edge of the bucket capped by the max", values: []time.Duration{3 * time.Microsecond}, q: 0.5, want: 3 * time.Microsecond, wantOK: true},
		{name: "answers the upper edge of the bucket holding the rank", values: []time.Duration{time.Microsecond, 2 * time.Microsecond, 3 * time.Microsecond, time.Millisecond}, q: 0.5, want: 2 * time.Microsecond, wantOK: true},
		{name: "counts merged values", values: []time.Duration{time.Microsecond}, merge: []time.Duration{time.Millisecond, time.Millisecond}, q: 0.99, want: time.Millisecond, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var h, o histogram.Histogram
			for _, v := range tt.values {
				h.Observe(v)
			}
			for _, v := range tt.merge {
				o.Observe(v)
			}
			h.Merge(o)
			got, ok := h.Quantile(tt.q)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
