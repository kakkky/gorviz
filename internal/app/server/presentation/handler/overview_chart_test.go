package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNiceRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		lo, hi float64
		unit   float64
		want   yScale
	}{
		{name: "success: goroutines up to 21 fit under 25", lo: 0, hi: 21, unit: 1, want: yScale{lo: 0, hi: 25, step: 5}},
		{name: "success: values far from zero fit tightly", lo: 4.3, hi: 4.9, unit: 1, want: yScale{lo: 4.2, hi: 5, step: 0.2}},
		{name: "success: values near zero start at zero", lo: 0.1, hi: 1.64, unit: 1, want: yScale{lo: 0, hi: 2, step: 0.5}},
		{name: "success: bytes are rounded in MiB", lo: 2.6 * (1 << 20), hi: 6.3 * (1 << 20), unit: 1 << 20, want: yScale{lo: 2 * (1 << 20), hi: 7 * (1 << 20), step: 1 << 20}},
		{name: "success: a single value gets some room", lo: 100, hi: 100, unit: 1, want: yScale{lo: 90, hi: 110, step: 5}},
		{name: "success: all zero", lo: 0, hi: 0, unit: 1, want: yScale{lo: 0, hi: 1, step: 0.2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := niceRange(tt.lo, tt.hi, tt.unit)
			assert.InDelta(t, tt.want.lo, got.lo, 1e-6)
			assert.InDelta(t, tt.want.hi, got.hi, 1e-6)
			assert.InDelta(t, tt.want.step, got.step, 1e-6)
			assert.True(t, got.contains(tt.lo) && got.contains(tt.hi), "the range holds every value")
		})
	}
}
