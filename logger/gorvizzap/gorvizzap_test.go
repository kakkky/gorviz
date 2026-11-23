package gorvizzap_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kakkky/gorviz/internal/protocol/loggerhook"
	"github.com/kakkky/gorviz/internal/protocol/loggerhook/loggerhooktest"
	"github.com/kakkky/gorviz/logger/gorvizzap"
)

func TestWrapCore(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "adds the seq to the line and the trace and forwards the written bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), gorvizzap.WrapWriteSyncer(zapcore.AddSync(&out)), zap.InfoLevel)
			logger := zap.New(gorvizzap.WrapCore(core)).With(zap.String("service", "api"))

			logger.Info("before tracing")
			res := loggerhooktest.Capture(t, func() {
				logger.Debug("disabled level")
				logger.Error("while tracing")
			})

			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			require.Len(t, lines, 2)
			require.Len(t, res.Marks, 1)
			require.Len(t, res.Forwarded, 1)
			assert.Equal(t, lines[1], strings.TrimSpace(string(res.Forwarded[0])))
			var line map[string]any
			require.NoError(t, json.Unmarshal([]byte(lines[1]), &line))
			assert.Equal(t, "while tracing", line["msg"])
			assert.InDelta(t, res.Marks[0], line[loggerhook.SeqKey], 0)
		})
	}
}
