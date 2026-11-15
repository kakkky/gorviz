package logrusadapter_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/agent/adapter/logparse"
	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/logmark/logmarktest"
	"github.com/kakkky/gorviz/logger/logrusadapter"
)

func TestLogrusAdapter(t *testing.T) {
	tests := []struct {
		name      string
		formatter logrus.Formatter
	}{
		{name: "success: JSON", formatter: &logrus.JSONFormatter{}},
		{name: "success: text", formatter: &logrus.TextFormatter{DisableColors: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			logger := logrus.New()
			logger.SetOutput(&out)
			logger.SetFormatter(logrusadapter.WrapFormatter(tt.formatter))

			logger.Info("before tracing")
			res := logmarktest.Capture(t, func() {
				logger.Debug("disabled level")
				logger.WithField("table", "users").Error("while tracing")
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
		})
	}
}
