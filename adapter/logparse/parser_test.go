package logparse_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/adapter/logparse"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
)

func TestParser_Parse(t *testing.T) {
	t.Parallel()
	received := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	jst := time.FixedZone("JST", 9*60*60)
	tests := []struct {
		name string
		raw  string
		want domain.LogLine
	}{
		{
			name: "success: slog JSON with trace seq",
			raw:  `{"time":"2026-11-15T11:29:52.123+09:00","level":"ERROR","msg":"db timeout","gorviz_seq":42}`,
			want: domain.LogLine{
				At: time.Date(2026, 11, 15, 11, 29, 52, 123_000_000, jst), TimeFromLog: true,
				Level: domain.LogLevelError, Message: "db timeout", TraceSeq: 42, HasTraceSeq: true,
			},
		},
		{
			name: "success: slog JSON duration in nanoseconds",
			raw:  `{"time":"2026-11-15T11:29:52.123+09:00","level":"INFO","msg":"request done","duration":1500000000}`,
			want: domain.LogLine{
				At: time.Date(2026, 11, 15, 11, 29, 52, 123_000_000, jst), TimeFromLog: true,
				Level: domain.LogLevelInfo, Message: "request done", Duration: 1500 * time.Millisecond, HasDuration: true,
			},
		},
		{
			name: "success: slog text duration as a string",
			raw:  `time=2026-11-15T11:29:52.5+09:00 level=INFO msg="request done" duration=250ms`,
			want: domain.LogLine{
				At: time.Date(2026, 11, 15, 11, 29, 52, 500_000_000, jst), TimeFromLog: true,
				Level: domain.LogLevelInfo, Message: "request done", Duration: 250 * time.Millisecond, HasDuration: true,
			},
		},
		{
			name: "success: slog text",
			raw:  `time=2026-11-15T11:29:52.5+09:00 level=WARN msg="slow query" table=users gorviz_seq=7`,
			want: domain.LogLine{
				At: time.Date(2026, 11, 15, 11, 29, 52, 500_000_000, jst), TimeFromLog: true,
				Level: domain.LogLevelWarn, Message: "slow query", TraceSeq: 7, HasTraceSeq: true,
			},
		},
		{
			name: "success: standard log with microseconds",
			raw:  `2026/11/15 11:29:52.250000 listening on :8080`,
			want: domain.LogLine{
				At: time.Date(2026, 11, 15, 11, 29, 52, 250_000_000, time.Local), TimeFromLog: true,
				Message: "listening on :8080",
			},
		},
		{
			name: "success: slog default logger through standard log",
			raw:  `2026/11/15 11:29:52 INFO request done status=200`,
			want: domain.LogLine{
				At: time.Date(2026, 11, 15, 11, 29, 52, 0, time.Local), TimeFromLog: true,
				Level: domain.LogLevelInfo, Message: "request done",
			},
		},
		{
			name: "success: unknown format falls back to received time",
			raw:  `panic: something went wrong`,
			want: domain.LogLine{At: received, Message: "panic: something went wrong"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := logparse.NewParser(&config.Config{DurationKeys: []string{"duration"}}).Parse(tt.raw, "stderr", received)
			tt.want.Raw, tt.want.Source = tt.raw, "stderr"
			assert.True(t, tt.want.At.Equal(got.At), "at: want %v, got %v", tt.want.At, got.At)
			got.At = tt.want.At
			assert.Equal(t, tt.want, got)
		})
	}
}
