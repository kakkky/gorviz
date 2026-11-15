package runtimeparse

import (
	"strings"
	"testing"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGoroutineDump(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		dump string
		want []model.Goroutine
	}{
		{
			name: "success: running main goroutine without creator",
			dump: `goroutine 1 [running]:
main.main()
	/src/main.go:10 +0x1d
`,
			want: []model.Goroutine{{ID: 1, State: "running", EntryFunction: "main.main", CurrentFunction: "main.main"}},
		},
		{
			name: "success: waiting goroutine with wait minutes and creator",
			dump: `goroutine 18 [chan receive, 5 minutes]:
main.leakedReceive()
	/src/main.go:42 +0x2c
created by main.leakForever in goroutine 1
	/src/main.go:36 +0x1a
`,
			want: []model.Goroutine{{
				ID: 18, State: "chan receive", Wait: 5 * time.Minute,
				EntryFunction: "main.leakedReceive", CurrentFunction: "main.leakedReceive", CreatedBy: "main.leakForever", CreatedAt: "/src/main.go:36",
			}},
		},
		{
			name: "success: goroutine started by a wrapper library has the user's function as entry and origin, and labels are read",
			dump: `goroutine 21 [select] {scope: checkout, handler: /users}:
main.fetchUser.func1({0x0, 0x0})
	/src/main.go:50 +0x2c
github.com/kakkky/scope.(*Scope).Go.func1()
	/mod/scope/scope.go:80 +0x40
created by github.com/kakkky/scope.(*Scope).Go in goroutine 1
	/mod/scope/scope.go:75 +0x1a
`,
			want: []model.Goroutine{{
				ID: 21, State: "select", Labels: map[string]string{"scope": "checkout", "handler": "/users"},
				EntryFunction: "main.fetchUser.func1", CurrentFunction: "main.fetchUser.func1", CreatedBy: "github.com/kakkky/scope.(*Scope).Go", CreatedAt: "/mod/scope/scope.go:75",
			}},
		},
		{
			name: "success: leaked goroutine marked by goroutineleak profile",
			dump: `goroutine 7 [chan receive (leaked)]:
main.leakedReceive()
	/src/main.go:42 +0x2c
created by main.leakForever in goroutine 1
	/src/main.go:36 +0x1a
`,
			want: []model.Goroutine{{
				ID: 7, State: "chan receive", Leaked: true,
				EntryFunction: "main.leakedReceive", CurrentFunction: "main.leakedReceive", CreatedBy: "main.leakForever", CreatedAt: "/src/main.go:36",
			}},
		},
		{
			name: "success: extra attributes like locked to thread and gp/m fields",
			dump: `goroutine 3 gp=0x14000002c40 m=nil [select, 1 minute, locked to thread]:
runtime.gopark(0x0?, 0x0?)
	/go/src/runtime/proc.go:435 +0xc8

goroutine 4 [runnable]:
main.cpuWorker()
	/src/main.go:20 +0x10
created by main.main in goroutine 1
	/src/main.go:14 +0x30
`,
			want: []model.Goroutine{
				{ID: 3, State: "select", Wait: time.Minute, EntryFunction: "runtime.gopark", CurrentFunction: "runtime.gopark"},
				{ID: 4, State: "runnable", EntryFunction: "main.cpuWorker", CurrentFunction: "main.cpuWorker", CreatedBy: "main.main", CreatedAt: "/src/main.go:14"},
			},
		},
		{
			name: "success: entry is the bottom frame and current skips runtime frames",
			dump: `goroutine 9 [chan receive, 3 minutes]:
runtime.gopark(0x0?, 0x0?, 0x0?, 0x0?, 0x0?)
	/go/src/runtime/proc.go:435 +0xc8
runtime.chanrecv(0x0?, 0x0?, 0x0?)
	/go/src/runtime/chan.go:664 +0x42c
main.fetch(...)
	/src/main.go:50
main.handle.func1()
	/src/main.go:40 +0x30
created by main.handle in goroutine 1
	/src/main.go:38 +0x1a
`,
			want: []model.Goroutine{{
				ID: 9, State: "chan receive", Wait: 3 * time.Minute,
				EntryFunction: "main.handle.func1", CurrentFunction: "main.fetch", CreatedBy: "main.handle", CreatedAt: "/src/main.go:38",
			}},
		},
		{
			name: "success: header with pprof labels is still parsed",
			dump: `goroutine 12 [select] {handler: /users, tenant: "a b"}:
main.serve()
	/src/main.go:70 +0x30
created by main.main in goroutine 1
	/src/main.go:20 +0x1a
`,
			want: []model.Goroutine{{
				ID: 12, State: "select", Labels: map[string]string{"handler": "/users", "tenant": "a b"},
				EntryFunction: "main.serve", CurrentFunction: "main.serve", CreatedBy: "main.main", CreatedAt: "/src/main.go:20",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewParser().ParseGoroutineDump(strings.NewReader(tt.dump))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
