package zapadapter_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kakkky/gorviz/internal/adapter/logparse"
	"github.com/kakkky/gorviz/internal/config"
	"github.com/kakkky/gorviz/internal/logmark/logmarktest"
	"github.com/kakkky/gorviz/logger/zapadapter"
)

func TestZapAdapter(t *testing.T) {
	var out bytes.Buffer
	core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapadapter.WrapWriteSyncer(zapcore.AddSync(&out)), zap.InfoLevel)
	logger := zap.New(zapadapter.WrapCore(core)).With(zap.String("service", "api"))

	logger.Info("before tracing")
	res := logmarktest.Capture(t, func() {
		logger.Debug("disabled level")
		logger.Error("while tracing")
	})

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 2)
	require.Len(t, res.Marks, 1, "only the written line is marked")
	require.Len(t, res.Forwarded, 1, "the line is passed to the embedded agent")
	assert.Equal(t, lines[1], strings.TrimSpace(string(res.Forwarded[0])), "the agent gets the line as the app wrote it")

	got := logparse.NewParser(&config.Config{}).Parse(lines[1], "app", time.Now())
	assert.Equal(t, "while tracing", got.Message)
	assert.True(t, got.HasTraceSeq)
	assert.Equal(t, res.Marks[0], got.TraceSeq, "the same seq is in the log line and the trace")
}
