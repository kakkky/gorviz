package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestParseLogLine(t *testing.T) {
	t.Parallel()
	received := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	jst := time.FixedZone("JST", 9*60*60)
	tests := []struct {
		name string
		raw  string
		want observation.LogLine
	}{
		{
			name: "reads slog JSON with the trace seq",
			raw:  `{"time":"2026-11-15T11:29:52.123+09:00","level":"ERROR","msg":"db timeout","gorviz_seq":42}`,
			want: observation.LogLine{At: time.Date(2026, 11, 15, 11, 29, 52, 123_000_000, jst), TimeFromLog: true, Level: observation.LogLevelError, Message: "db timeout", TraceSeq: 42, HasTraceSeq: true},
		},
		{
			name: "finds the trace seq inside a slog group",
			raw:  `{"time":"2026-11-15T11:29:52.123+09:00","level":"INFO","msg":"done","req":{"id":"a","gorviz_seq":9}}`,
			want: observation.LogLine{At: time.Date(2026, 11, 15, 11, 29, 52, 123_000_000, jst), TimeFromLog: true, Level: observation.LogLevelInfo, Message: "done", TraceSeq: 9, HasTraceSeq: true},
		},
		{
			name: "reads a slog JSON duration in nanoseconds",
			raw:  `{"time":"2026-11-15T11:29:52.123+09:00","level":"INFO","msg":"request done","duration":1500000000}`,
			want: observation.LogLine{At: time.Date(2026, 11, 15, 11, 29, 52, 123_000_000, jst), TimeFromLog: true, Level: observation.LogLevelInfo, Message: "request done", Duration: 1500 * time.Millisecond, HasDuration: true},
		},
		{
			name: "reads slog text with a duration string",
			raw:  `time=2026-11-15T11:29:52.5+09:00 level=INFO msg="request done" duration=250ms`,
			want: observation.LogLine{At: time.Date(2026, 11, 15, 11, 29, 52, 500_000_000, jst), TimeFromLog: true, Level: observation.LogLevelInfo, Message: "request done", Duration: 250 * time.Millisecond, HasDuration: true},
		},
		{
			name: "finds the trace seq inside a slog text group",
			raw:  `time=2026-11-15T11:29:52.5+09:00 level=WARN msg="slow query" req.gorviz_seq=7`,
			want: observation.LogLine{At: time.Date(2026, 11, 15, 11, 29, 52, 500_000_000, jst), TimeFromLog: true, Level: observation.LogLevelWarn, Message: "slow query", TraceSeq: 7, HasTraceSeq: true},
		},
		{
			name: "reads zap production JSON",
			raw:  `{"level":"error","ts":1794713392.5,"caller":"app/main.go:10","msg":"db timeout","gorviz_seq":3}`,
			want: observation.LogLine{At: time.Unix(1794713392, 500_000_000), TimeFromLog: true, Level: observation.LogLevelError, Message: "db timeout", TraceSeq: 3, HasTraceSeq: true},
		},
		{
			name: "reads the zap console encoder",
			raw:  "2026-11-15T11:29:52.123+0900\tWARN\tapp/main.go:10\tslow query\t{\"gorviz_seq\": 4}",
			want: observation.LogLine{At: time.Date(2026, 11, 15, 11, 29, 52, 123_000_000, jst), TimeFromLog: true, Level: observation.LogLevelWarn, Message: "slow query", TraceSeq: 4, HasTraceSeq: true},
		},
		{
			name: "reads zerolog JSON",
			raw:  `{"level":"warn","gorviz_seq":5,"time":"2026-11-15T11:29:52+09:00","message":"slow query"}`,
			want: observation.LogLine{At: time.Date(2026, 11, 15, 11, 29, 52, 0, jst), TimeFromLog: true, Level: observation.LogLevelWarn, Message: "slow query", TraceSeq: 5, HasTraceSeq: true},
		},
		{
			name: "reads logrus text",
			raw:  `time="2026-11-15T11:29:52+09:00" level=warning msg="slow query" gorviz_seq=8`,
			want: observation.LogLine{At: time.Date(2026, 11, 15, 11, 29, 52, 0, jst), TimeFromLog: true, Level: observation.LogLevelWarn, Message: "slow query", TraceSeq: 8, HasTraceSeq: true},
		},
		{
			name: "reads the standard log with microseconds",
			raw:  `2026/11/15 11:29:52.250000 listening on :8080`,
			want: observation.LogLine{At: time.Date(2026, 11, 15, 11, 29, 52, 250_000_000, time.Local), TimeFromLog: true, Message: "listening on :8080"},
		},
		{
			name: "reads the slog default logger through the standard log",
			raw:  `2026/11/15 11:29:52 INFO request done status=200`,
			want: observation.LogLine{At: time.Date(2026, 11, 15, 11, 29, 52, 0, time.Local), TimeFromLog: true, Level: observation.LogLevelInfo, Message: "request done"},
		},
		{
			name: "falls back to the received time for an unknown format",
			raw:  `panic: something went wrong`,
			want: observation.LogLine{At: received, Message: "panic: something went wrong"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ParseLogLine(tt.raw, observation.LogSourceFile, received, []string{"duration"})
			tt.want.Raw, tt.want.Source = tt.raw, observation.LogSourceFile
			assert.True(t, tt.want.At.Equal(got.At), "want %v, got %v", tt.want.At, got.At)
			got.At = tt.want.At
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseLogLine_FromLogRouter(t *testing.T) {
	t.Parallel()
	received := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		raw  string
		want observation.LogLine
	}{
		{
			name: "takes the app's line out of a FireLens record",
			raw:  `{"container_name":"app","source":"stdout","log":"{\"level\":\"ERROR\",\"msg\":\"db timeout\"}\n"}`,
			want: observation.LogLine{At: received, Level: observation.LogLevelError, Message: "db timeout", Raw: `{"level":"ERROR","msg":"db timeout"}`, Source: observation.LogSourceTCP},
		},
		{
			name: "keeps an app's own JSON line that happens to have a log key",
			raw:  `{"level":"INFO","msg":"ok","log":"x"}`,
			want: observation.LogLine{At: received, Level: observation.LogLevelInfo, Message: "ok", Raw: `{"level":"INFO","msg":"ok","log":"x"}`, Source: observation.LogSourceTCP},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ParseLogLine(tt.raw, observation.LogSourceTCP, received, nil)
			assert.True(t, tt.want.At.Equal(got.At), "want %v, got %v", tt.want.At, got.At)
			got.At = tt.want.At
			assert.Equal(t, tt.want, got)
		})
	}
}
