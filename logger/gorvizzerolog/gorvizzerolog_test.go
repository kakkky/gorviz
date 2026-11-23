package gorvizzerolog_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/protocol/loggerhook"
	"github.com/kakkky/gorviz/internal/protocol/loggerhook/loggerhooktest"
	"github.com/kakkky/gorviz/logger/gorvizzerolog"
)

func TestHook(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "adds the seq to the line and the trace and forwards the written bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			logger := zerolog.New(gorvizzerolog.WrapWriter(&out)).Level(zerolog.InfoLevel).With().Timestamp().Logger().Hook(gorvizzerolog.Hook())

			logger.Info().Msg("before tracing")
			res := loggerhooktest.Capture(t, func() {
				logger.Debug().Msg("disabled level")
				logger.Error().Str("table", "users").Msg("while tracing")
			})

			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			require.Len(t, lines, 2)
			require.Len(t, res.Marks, 1)
			require.Len(t, res.Forwarded, 1)
			assert.Equal(t, lines[1], strings.TrimSpace(string(res.Forwarded[0])))
			var line map[string]any
			require.NoError(t, json.Unmarshal([]byte(lines[1]), &line))
			assert.Equal(t, "while tracing", line["message"])
			assert.InDelta(t, res.Marks[0], line[loggerhook.SeqKey], 0)
		})
	}
}
