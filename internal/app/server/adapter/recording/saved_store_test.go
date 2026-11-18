package recording_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/adapter/recording"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestSavedStore_Prune(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	parse := func(raw []byte, receivedAt time.Time) (model.TraceChunk, error) {
		return model.TraceChunk{StartedAt: receivedAt}, nil
	}
	rec := func(id string, to time.Time) model.SavedRecording {
		return model.SavedRecording{ID: id, At: to, From: to.Add(-time.Second), To: to, Chunks: 1}
	}
	chunk := []model.TraceChunk{{Raw: []byte("trace")}}

	s, err := recording.NewSavedStore(dir, parse, now)
	require.NoError(t, err)
	require.NoError(t, s.Save(rec("old", now.Add(-domain.IssueRetention-time.Hour)), chunk))
	require.NoError(t, s.Save(rec("new", now.Add(-time.Hour)), chunk))

	require.NoError(t, s.Prune(now))

	assert.False(t, s.Has("old"), "a recording older than the retention is dropped")
	assert.True(t, s.Has("new"))
	_, err = os.Stat(filepath.Join(dir, "old"))
	assert.True(t, os.IsNotExist(err), "and its files are removed")

	reloaded, err := recording.NewSavedStore(dir, parse, now.Add(domain.IssueRetention))
	require.NoError(t, err)
	assert.False(t, reloaded.Has("new"), "a recording that expired while the server was down is not loaded")
}
