package domain_test

import (
	"testing"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueBook(t *testing.T) {
	t.Parallel()
	lock := func(from, to time.Duration) domain.Detection {
		return domain.Detection{Kind: domain.IssueLockContention, From: from, To: to, Certainty: domain.CertaintyInference}
	}
	tests := []struct {
		name  string
		run   func(b *domain.IssueBook)
		check func(t *testing.T, b *domain.IssueBook)
	}{
		{
			name: "extends an occurrence that continues without a gap",
			run: func(b *domain.IssueBook) {
				b.Record(lock(0, time.Second), "")
				b.Record(lock(time.Second, 2*time.Second), "smoke")
			},
			check: func(t *testing.T, b *domain.IssueBook) {
				require.Len(t, b.Issues(), 1)
				require.Len(t, b.Issues()[0].Occurrences, 1)
				assert.Equal(t, 2*time.Second, b.Issues()[0].Occurrences[0].To)
				assert.Equal(t, "smoke", b.Issues()[0].FirstPhase())
			},
		},
		{
			name: "starts a new occurrence after a gap and keeps one issue per kind and target",
			run: func(b *domain.IssueBook) {
				b.Record(lock(0, time.Second), "")
				b.Record(lock(5*time.Second, 6*time.Second), "")
				b.Record(domain.Detection{Kind: domain.IssueLongSTW, Target: "GC mark termination", From: 0, To: time.Second}, "")
			},
			check: func(t *testing.T, b *domain.IssueBook) {
				require.Len(t, b.Issues(), 2)
				assert.Len(t, b.Issues()[0].Occurrences, 2)
				assert.Equal(t, domain.IssueID("I2"), b.Issues()[1].ID)
			},
		},
		{
			name: "tags the phase on an occurrence that the phase judgment overlaps",
			run: func(b *domain.IssueBook) {
				b.Record(lock(2*time.Second, 3*time.Second), "")
				b.RecordPhase(lock(time.Second, 4*time.Second), "ramp-up 2")
			},
			check: func(t *testing.T, b *domain.IssueBook) {
				require.Len(t, b.Issues()[0].Occurrences, 1)
				assert.Equal(t, "ramp-up 2", b.Issues()[0].Occurrences[0].Phase)
			},
		},
		{
			name: "answers a missing issue with the issues that exist",
			run: func(b *domain.IssueBook) {
				b.Record(lock(0, time.Second), "")
			},
			check: func(t *testing.T, b *domain.IssueBook) {
				err := domain.IssueNotFound("I9", b.Issues())
				require.ErrorIs(t, err, domain.ErrNotFound)
				assert.Contains(t, err.Error(), "I1")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := domain.NewIssueBook()
			tt.run(b)
			tt.check(t, b)
		})
	}
}
