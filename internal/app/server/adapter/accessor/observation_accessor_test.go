package accessor_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
)

func TestObservationAccessor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		put      []int
		from, to time.Duration
		want     []int
		wantErr  error
	}{
		{name: "lists the seconds the range touches in order", put: []int{2, 0, 1, 3}, from: 500 * time.Millisecond, to: 2500 * time.Millisecond, want: []int{0, 1, 2}},
		{name: "does not include the second that starts at the end of the range", put: []int{0, 1, 2}, from: 0, to: 2 * time.Second, want: []int{0, 1}},
		{name: "lists the one second of an instant", put: []int{0, 1}, from: 1500 * time.Millisecond, to: 1500 * time.Millisecond, want: []int{1}},
		{name: "skips the seconds not closed yet", put: []int{0}, from: 0, to: 3 * time.Second, want: []int{0}},
		{name: "rejects a range that ends before it starts", from: 2 * time.Second, to: time.Second, wantErr: domain.ErrInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := accessor.NewObservationAccessor(kvs.NewInMemoryKVS())
			for _, s := range tt.put {
				require.NoError(t, a.Put(domain.Observation{Second: s}))
			}
			got, err := a.List(tt.from, tt.to)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			var seconds []int
			for _, o := range got {
				seconds = append(seconds, o.Second)
			}
			assert.Equal(t, tt.want, seconds)
		})
	}
}

func TestIssueBookAccessor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		check func(t *testing.T, a *accessor.IssueBookAccessor)
	}{
		{
			name: "keeps what was read unchanged while the book is updated",
			check: func(t *testing.T, a *accessor.IssueBookAccessor) {
				a.UpdateIssueBook(func(b *domain.IssueBook) {
					b.Record(domain.Detection{Kind: domain.IssueLockContention, From: 0, To: time.Second}, "")
				})
				var before *domain.IssueBook
				a.ViewIssueBook(func(b *domain.IssueBook) { before = b })
				a.UpdateIssueBook(func(b *domain.IssueBook) {
					b.Record(domain.Detection{Kind: domain.IssueLockContention, From: 5 * time.Second, To: 6 * time.Second}, "")
				})
				assert.Len(t, before.Issues()[0].Occurrences, 1)
				a.ViewIssueBook(func(b *domain.IssueBook) { assert.Len(t, b.Issues()[0].Occurrences, 2) })
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, accessor.NewIssueBookAccessor(kvs.NewInMemoryKVS()))
		})
	}
}
