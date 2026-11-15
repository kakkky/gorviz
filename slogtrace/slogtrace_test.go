package slogtrace_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"runtime/trace"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	xtrace "golang.org/x/exp/trace"

	"github.com/kakkky/gorviz/slogtrace"
)

func TestWrap(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slogtrace.Wrap(slog.NewJSONHandler(&out, nil)))

	logger.Info("before tracing")
	var buf bytes.Buffer
	require.NoError(t, trace.Start(&buf))
	logger.Info("while tracing")
	trace.Stop()

	var lines []map[string]any
	dec := json.NewDecoder(&out)
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		lines = append(lines, m)
	}
	require.Len(t, lines, 2)
	assert.NotContains(t, lines[0], slogtrace.SeqAttrKey, "no seq is attached while trace is disabled")
	seq, ok := lines[1][slogtrace.SeqAttrKey].(float64)
	require.True(t, ok, "seq is attached while trace is enabled")

	r, err := xtrace.NewReader(&buf)
	require.NoError(t, err)
	var marks []string
	for {
		ev, err := r.ReadEvent()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if ev.Kind() == xtrace.EventLog && ev.Log().Category == slogtrace.TraceCategory {
			marks = append(marks, ev.Log().Message)
		}
	}
	assert.Equal(t, []string{strconv.FormatUint(uint64(seq), 10)}, marks)
}
