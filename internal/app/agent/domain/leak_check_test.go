package domain_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestParseGoroutineLeakProfile(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		file    string
		raw     string
		want    observation.GoroutineLeakCheck
		wantErr error
	}{
		{
			name: "reads the count and the stack of each leaked stack",
			file: "goroutineleak.txt",
			want: observation.GoroutineLeakCheck{At: at, Status: observation.LeakCheckDone, Leaks: []observation.LeakedStack{
				{Count: 3, Stack: []observation.Frame{{Function: "main.leakOnChannel", File: "/app/leak.go", Line: 11}}},
				{Count: 1, Stack: []observation.Frame{
					{Function: "internal/sync.runtime_SemacquireMutex", File: "/usr/local/go/src/runtime/sema.go", Line: 95},
					{Function: "internal/sync.(*Mutex).lockSlow", File: "/usr/local/go/src/internal/sync/mutex.go", Line: 149},
					{Function: "internal/sync.(*Mutex).Lock", File: "/usr/local/go/src/internal/sync/mutex.go", Line: 70},
					{Function: "sync.(*Mutex).Lock", File: "/usr/local/go/src/sync/mutex.go", Line: 46},
					{Function: "main.leakOnMutex", File: "/app/leak.go", Line: 13},
				}},
			}},
		},
		{
			name: "reads a profile without leaks",
			file: "goroutineleak_none.txt",
			want: observation.GoroutineLeakCheck{At: at, Status: observation.LeakCheckDone},
		},
		{
			name:    "rejects another profile",
			raw:     "goroutine profile: total 3\n",
			wantErr: domain.ErrInvalidArgument,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := []byte(tt.raw)
			if tt.file != "" {
				var err error
				raw, err = os.ReadFile(filepath.Join("testdata", tt.file))
				require.NoError(t, err)
			}
			got, err := domain.ParseGoroutineLeakProfile(raw, at)
			if tt.wantErr != nil {
				require.True(t, errors.Is(err, tt.wantErr))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPlanLeakCheck(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		last     time.Time
		interval time.Duration
		want     domain.LeakCheckPlan
	}{
		{name: "fetches the first time", interval: time.Minute, want: domain.LeakCheckFetch},
		{name: "waits for the interval", last: now.Add(-30 * time.Second), interval: time.Minute, want: domain.LeakCheckSkip},
		{name: "fetches after the interval", last: now.Add(-time.Minute), interval: time.Minute, want: domain.LeakCheckFetch},
		{name: "reports once that it is disabled", want: domain.LeakCheckReportDisabled},
		{name: "stays quiet after reporting it is disabled", last: now.Add(-time.Hour), want: domain.LeakCheckSkip},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.PlanLeakCheck(tt.last, now, tt.interval))
		})
	}
}

func TestParseThreadCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		file    string
		raw     string
		want    int
		wantErr error
	}{
		{name: "reads the total of the threadcreate profile", file: "threadcreate.txt", want: 7},
		{name: "rejects another profile", raw: "heap profile: 1: 2 [3: 4] @ heap/1048576\n", wantErr: domain.ErrInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := []byte(tt.raw)
			if tt.file != "" {
				var err error
				raw, err = os.ReadFile(filepath.Join("testdata", tt.file))
				require.NoError(t, err)
			}
			got, err := domain.ParseThreadCount(raw)
			if tt.wantErr != nil {
				require.True(t, errors.Is(err, tt.wantErr))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
