package recording_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/adapter/recording"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestSavedStore_Prune(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rec := func(id string, to time.Time) model.SavedRecording {
		return model.SavedRecording{ID: id, At: to, From: to.Add(-time.Second), To: to, Chunks: 1}
	}
	chunk := []model.TraceChunk{{StartedAt: now, Timeline: model.SchedulerTimeline{Window: time.Second}}}

	s, err := recording.NewSavedStore(dir, now)
	require.NoError(t, err)
	require.NoError(t, s.Save(rec("old", now.Add(-domain.IssueRetention-time.Hour)), chunk))
	require.NoError(t, s.Save(rec("new", now.Add(-time.Hour)), chunk))

	require.NoError(t, s.Prune(now))

	assert.False(t, s.Has("old"), "a recording older than the retention is dropped")
	assert.True(t, s.Has("new"))
	_, err = os.Stat(filepath.Join(dir, "old"))
	assert.True(t, os.IsNotExist(err), "and its files are removed")

	reloaded, err := recording.NewSavedStore(dir, now.Add(domain.IssueRetention))
	require.NoError(t, err)
	assert.False(t, reloaded.Has("new"), "a recording that expired while the server was down is not loaded")
}

func TestSavedStore_ReloadsChunks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	chunks := []model.TraceChunk{{
		StartedAt: now.Add(-2 * time.Second).Local(),
		Timeline: model.SchedulerTimeline{
			Window: 2 * time.Second,
			Events: []model.SchedEvent{{At: time.Millisecond, Kind: model.SchedEventGoroutine, Goroutine: 7, Place: model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "sync.Mutex.Lock"}}},
		},
		LogMarks: []model.TraceLogMark{{Seq: 3, Goroutine: 7, At: time.Millisecond}},
	}}
	s, err := recording.NewSavedStore(dir, now)
	require.NoError(t, err)
	require.NoError(t, s.Save(model.SavedRecording{ID: "r", At: now, From: now.Add(-2 * time.Second), To: now, Chunks: 1}, chunks))

	reloaded, err := recording.NewSavedStore(dir, now)
	require.NoError(t, err)
	got := recording.NewCompositeTraceRecording(recording.NewTraceStore(1<<20), reloaded).Between(now.Add(-time.Second), now)

	assert.Equal(t, chunks, got, "the chunks read back are the ones that were saved")
}

func TestSavedStore_SkipsRawTraceRecordings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "old"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "old", "meta.json"), []byte(`{"ID":"old","To":"2026-11-16T11:00:00Z","Chunks":1}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "old", "chunk-0.trace"), []byte("raw"), 0o644))

	s, err := recording.NewSavedStore(dir, now)

	require.NoError(t, err, "a recording saved as raw traces does not stop the server")
	assert.False(t, s.Has("old"))
}
