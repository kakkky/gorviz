package gorviz_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz"
	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/logmark/logmarktest"
)

func TestNewSlogHandler(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(gorviz.NewSlogHandler(slog.NewTextHandler(&out, nil)))

	logger.Info("before tracing")
	res := logmarktest.Capture(t, func() { logger.Info("while tracing") })

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 2)
	assert.NotContains(t, lines[0], model.LogSeqAttrKey, "no seq is attached while trace is disabled")
	require.Len(t, res.Marks, 1)
	assert.Contains(t, lines[1], model.LogSeqAttrKey+"="+strconv.FormatUint(res.Marks[0], 10), "the same seq is in the log line and the trace")

	require.Len(t, res.Forwarded, 1, "the line is passed to the embedded agent")
	var forwarded map[string]any
	require.NoError(t, json.Unmarshal(res.Forwarded[0], &forwarded))
	assert.Equal(t, "while tracing", forwarded["msg"])
	assert.InDelta(t, res.Marks[0], forwarded[model.LogSeqAttrKey], 0)
}
