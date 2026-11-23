package gorvizslog_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/protocol/loggerhook"
	"github.com/kakkky/gorviz/internal/protocol/loggerhook/loggerhooktest"
	"github.com/kakkky/gorviz/logger/gorvizslog"
)

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name     string
		logger   func(h slog.Handler) *slog.Logger
		wantText string
		seqOf    func(forwarded map[string]any) any
	}{
		{
			name:     "adds the seq to the line and the trace and forwards the line as JSON",
			logger:   func(h slog.Handler) *slog.Logger { return slog.New(h) },
			wantText: loggerhook.SeqKey + "=",
			seqOf:    func(m map[string]any) any { return m[loggerhook.SeqKey] },
		},
		{
			name:     "puts the seq inside the group after WithGroup",
			logger:   func(h slog.Handler) *slog.Logger { return slog.New(h).WithGroup("req") },
			wantText: "req." + loggerhook.SeqKey + "=",
			seqOf:    func(m map[string]any) any { return m["req"].(map[string]any)[loggerhook.SeqKey] },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			logger := tt.logger(gorvizslog.NewHandler(slog.NewTextHandler(&out, nil)))

			logger.Info("before tracing")
			res := loggerhooktest.Capture(t, func() {
				logger.Debug("disabled level")
				logger.Info("while tracing")
			})

			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			require.Len(t, lines, 2)
			assert.NotContains(t, lines[0], loggerhook.SeqKey)
			require.Len(t, res.Marks, 1)
			assert.Contains(t, lines[1], tt.wantText+strconv.FormatUint(res.Marks[0], 10))
			require.Len(t, res.Forwarded, 1)
			var forwarded map[string]any
			require.NoError(t, json.Unmarshal(res.Forwarded[0], &forwarded))
			assert.Equal(t, "while tracing", forwarded["msg"])
			assert.InDelta(t, res.Marks[0], tt.seqOf(forwarded), 0)
		})
	}
}
