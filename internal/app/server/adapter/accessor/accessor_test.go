package accessor_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

var (
	inst = observation.Instance{Service: "checkout", Name: "checkout-1", Incarnation: "inc-1"}
	t0   = time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
)

// openRoot は保存先を開く。返した関数で閉じ、閉じなければテストの終わりに閉じる
func openRoot(t *testing.T, dir string) (*filesystem.Root, func()) {
	t.Helper()
	root, cleanup, err := filesystem.NewRoot(dir)
	require.NoError(t, err)
	closed := false
	closeRoot := func() {
		if !closed {
			closed = true
			cleanup()
		}
	}
	t.Cleanup(closeRoot)
	return root, closeRoot
}

func logLine(seq uint64) observation.LogLine {
	return observation.LogLine{Agent: "a1", Seq: seq, At: t0.Add(time.Duration(seq) * time.Second), Level: observation.LogLevelInfo, Message: "hello"}
}

func seqs(logs []domain.StoredLog) []uint64 {
	var out []uint64
	for _, l := range logs {
		out = append(out, l.Seq)
	}
	return out
}

func TestLogAccessor(t *testing.T) {
	t.Parallel()
	logFile := "services/checkout/service/logs/2026-11-21T14.jsonl"
	tests := []struct {
		name   string
		damage func(t *testing.T, file string)
		want   []uint64
	}{
		{name: "reads back what it appended in order", damage: func(*testing.T, string) {}, want: []uint64{1, 2, 3}},
		{
			name: "truncates a torn tail before appending again",
			damage: func(t *testing.T, file string) {
				f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
				require.NoError(t, err)
				_, err = f.WriteString(`0badc0de {"instance":"checkout-1"`)
				require.NoError(t, err)
				require.NoError(t, f.Close())
			},
			want: []uint64{1, 2, 3},
		},
		{
			name: "skips a line whose checksum does not match",
			damage: func(t *testing.T, file string) {
				b, err := os.ReadFile(file)
				require.NoError(t, err)
				i := len("00000000 ") + 10
				b[i] ^= 0x01
				require.NoError(t, os.WriteFile(file, b, 0o644))
			},
			want: []uint64{2, 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dir := t.TempDir()
			root, closeRoot := openRoot(t, dir)
			a := accessor.NewLogAccessor(root)
			require.NoError(t, a.AppendLogs(ctx, inst, []observation.LogLine{logLine(1), logLine(2)}))
			closeRoot()
			tt.damage(t, filepath.Join(dir, logFile))

			// 落ちてから起動し直したときと同じく、開き直した保存先で追記を始める
			root, _ = openRoot(t, dir)
			a = accessor.NewLogAccessor(root)
			require.NoError(t, a.AppendLogs(ctx, inst, []observation.LogLine{logLine(3)}))

			got, err := a.ListLogs(ctx, inst.Service, t0, t0.Add(time.Hour))
			require.NoError(t, err)
			assert.Equal(t, tt.want, seqs(got))
			assert.Equal(t, inst.Name, got[0].Instance)
		})
	}
}

func TestTraceAccessor(t *testing.T) {
	t.Parallel()
	chunk := func(sec int) observation.TraceChunk {
		return observation.TraceChunk{StartedAt: t0.Add(time.Duration(sec) * time.Second), Duration: 5 * time.Second, Threads: sec,
			Entries: map[observation.GoroutineID]string{1: "main.main"},
			Events:  []observation.StateChange{{At: time.Second, Kind: observation.StateChangeGoroutine, Goroutine: 1, Place: observation.Place{Kind: observation.PlaceRunning}}}}
	}
	tests := []struct {
		name     string
		from, to time.Time
		torn     bool
		want     []int
	}{
		{name: "reads only the chunks the index puts in the range", from: t0.Add(6 * time.Second), to: t0.Add(11 * time.Second), want: []int{5, 10}},
		{name: "reads a chunk that started in the previous hour", from: t0.Add(time.Hour), to: t0.Add(time.Hour + time.Second), want: []int{3598}},
		{name: "drops a chunk that has no index entry before appending", from: t0, to: t0.Add(20 * time.Second), torn: true, want: []int{0, 5, 10, 15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dir := t.TempDir()
			root, closeRoot := openRoot(t, dir)
			a := accessor.NewTraceAccessor(root)
			require.NoError(t, a.AppendTraceChunks(ctx, inst, []observation.TraceChunk{chunk(0), chunk(5), chunk(10), chunk(3598)}))
			if tt.torn {
				closeRoot()
				f, err := os.OpenFile(filepath.Join(dir, "services/checkout/instances/checkout-1/execution-traces/2026-11-21T14.pb.gz"), os.O_APPEND|os.O_WRONLY, 0)
				require.NoError(t, err)
				_, err = f.Write([]byte{0x1f, 0x8b, 0x08, 0x00, 0x01})
				require.NoError(t, err)
				require.NoError(t, f.Close())
				root, _ = openRoot(t, dir)
				a = accessor.NewTraceAccessor(root)
				require.NoError(t, a.AppendTraceChunks(ctx, inst, []observation.TraceChunk{chunk(15)}))
			}
			got, err := a.ListTraceChunks(ctx, inst.Service, inst.Name, tt.from, tt.to)
			require.NoError(t, err)
			var threads []int
			for _, c := range got {
				threads = append(threads, c.Threads)
				assert.Equal(t, "main.main", c.Entries[1])
				assert.Len(t, c.Events, 1)
			}
			assert.Equal(t, tt.want, threads)
		})
	}
}

func TestCheckpointAccessor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		updates map[domain.Process]domain.Checkpoint
	}{
		{name: "keeps the checkpoints of every process it updated", updates: map[domain.Process]domain.Checkpoint{
			domain.ProcessAggregateMetrics: {At: t0}, domain.ProcessDetectIssues: {At: t0.Add(time.Minute)},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			root, _ := openRoot(t, t.TempDir())
			a := accessor.NewCheckpointAccessor(root)
			for p, cp := range tt.updates {
				require.NoError(t, a.UpdateCheckpoint(ctx, inst.Service, p, string(inst.Name), cp))
			}
			got, err := a.ReadCheckpoints(ctx, inst.Service)
			require.NoError(t, err)
			for p, cp := range tt.updates {
				assert.True(t, cp.At.Equal(got.Get(p, string(inst.Name)).At))
			}
		})
	}
}
