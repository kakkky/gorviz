package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/domain"
)

func TestDetectLeaks(t *testing.T) {
	t.Parallel()
	const leaky = domain.GoroutineOrigin("main.leak @ main.go:10")
	const steady = domain.GoroutineOrigin("main.serve @ main.go:20")
	sample := func(leakyN, steadyN int) domain.GoroutineSample {
		return domain.GoroutineSample{WaitingByOrigin: map[domain.GoroutineOrigin]int{leaky: leakyN, steady: steadyN}}
	}
	policy := domain.LeakDetectionPolicy{MinSamples: 3, MinGrowth: 5}

	tests := []struct {
		name    string
		samples []domain.GoroutineSample
		current []domain.Goroutine
		want    []domain.Leak
	}{
		{
			name:    "success: origin whose waiting count never decreases and grows enough is a suspect",
			samples: []domain.GoroutineSample{sample(1, 3), sample(4, 3), sample(10, 3)},
			want:    []domain.Leak{{Origin: leaky, Count: 10, Growth: 9, Certainty: domain.CertaintyInference}},
		},
		{
			name:    "success: origin whose count decreased once is not a suspect",
			samples: []domain.GoroutineSample{sample(1, 3), sample(20, 3), sample(10, 3)},
			want:    []domain.Leak{},
		},
		{
			name:    "success: growth-based detection waits for enough samples",
			samples: []domain.GoroutineSample{sample(1, 3), sample(10, 3)},
			want:    []domain.Leak{},
		},
		{
			name:    "success: goroutines marked leaked by runtime are confirmed even without history",
			samples: nil,
			current: []domain.Goroutine{
				{ID: 1, State: "chan receive", CreatedBy: "main.leak", CreatedAt: "main.go:10", Leaked: true, Wait: 2 * time.Minute},
				{ID: 2, State: "chan receive", CreatedBy: "main.leak", CreatedAt: "main.go:10", Leaked: true},
				{ID: 3, State: "select", CreatedBy: "main.serve", CreatedAt: "main.go:20"},
			},
			want: []domain.Leak{{Origin: leaky, Count: 2, LongestWait: 2 * time.Minute, Certainty: domain.CertaintyFact}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.DetectLeaks(tt.samples, tt.current, policy))
		})
	}
}
