package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestProcLane_Buckets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		spans  []model.SchedSpan
		window time.Duration
		n      int
		want   []domain.LaneBucket
	}{
		{
			name:   "success: idle lane yields empty buckets",
			window: 4 * time.Millisecond,
			n:      2,
			want:   []domain.LaneBucket{{}, {}},
		},
		{
			name: "success: span crossing buckets is split by overlap",
			spans: []model.SchedSpan{
				{Goroutine: 7, Thread: 2, Start: 1 * time.Millisecond, End: 3 * time.Millisecond},
			},
			window: 4 * time.Millisecond,
			n:      2,
			want: []domain.LaneBucket{
				{Goroutine: 7, Thread: 2, Busy: 0.5},
				{Goroutine: 7, Thread: 2, Busy: 0.5},
			},
		},
		{
			name: "success: bucket represents the goroutine that ran longest in it",
			spans: []model.SchedSpan{
				{Goroutine: 1, Thread: 1, Start: 0, End: 500 * time.Microsecond},
				{Goroutine: 2, Thread: 3, Start: 500 * time.Microsecond, End: 2 * time.Millisecond},
			},
			window: 2 * time.Millisecond,
			n:      1,
			want:   []domain.LaneBucket{{Goroutine: 2, Thread: 3, Busy: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lane := model.ProcLane{Proc: 0, Spans: tt.spans}
			assert.Equal(t, tt.want, domain.Buckets(lane, tt.window, tt.n))
		})
	}
}
