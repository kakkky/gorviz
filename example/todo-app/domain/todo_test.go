package domain_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

func TestNewTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    domain.Title
		wantErr error
	}{
		{name: "trims spaces around the title", in: "  牛乳を買う ", want: "牛乳を買う"},
		{name: "rejects an empty title", in: "  ", wantErr: domain.ErrInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.NewTitle(tt.in)
			if tt.wantErr != nil {
				require.True(t, errors.Is(err, tt.wantErr))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComputeStats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		todos []domain.Todo
		want  domain.Stats
	}{
		{
			name:  "counts titles that differ only by the number as duplicates",
			todos: []domain.Todo{{Title: "牛乳を買う #1", Done: true}, {Title: "牛乳を買う #2"}, {Title: "本を返す #3"}},
			want:  domain.Stats{Total: 3, Done: 1, Duplicates: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.ComputeStats(tt.todos))
		})
	}
}
