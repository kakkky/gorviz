package gorvizlogrus_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/protocol/loggerhook"
	"github.com/kakkky/gorviz/internal/protocol/loggerhook/loggerhooktest"
	"github.com/kakkky/gorviz/logger/gorvizlogrus"
)

func TestWrapFormatter(t *testing.T) {
	tests := []struct {
		name      string
		formatter logrus.Formatter
		wantSeq   func(n uint64) string
	}{
		{
			name:      "adds the seq to a JSON line and forwards the formatted bytes",
			formatter: &logrus.JSONFormatter{},
			wantSeq:   func(n uint64) string { return `"` + loggerhook.SeqKey + `":` + strconv.FormatUint(n, 10) },
		},
		{
			name:      "adds the seq to a text line and forwards the formatted bytes",
			formatter: &logrus.TextFormatter{DisableColors: true},
			wantSeq:   func(n uint64) string { return loggerhook.SeqKey + "=" + strconv.FormatUint(n, 10) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			logger := logrus.New()
			logger.SetOutput(&out)
			logger.SetFormatter(gorvizlogrus.WrapFormatter(tt.formatter))

			logger.Info("before tracing")
			res := loggerhooktest.Capture(t, func() {
				logger.Debug("disabled level")
				logger.WithField("table", "users").Error("while tracing")
			})

			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			require.Len(t, lines, 2)
			assert.NotContains(t, lines[0], loggerhook.SeqKey)
			require.Len(t, res.Marks, 1)
			require.Len(t, res.Forwarded, 1)
			assert.Equal(t, lines[1], strings.TrimSpace(string(res.Forwarded[0])))
			assert.Contains(t, lines[1], tt.wantSeq(res.Marks[0]))
		})
	}
}
