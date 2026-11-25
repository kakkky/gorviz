package domain_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestIssueFunction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, fn, want string
	}{
		{name: "keeps a named function", fn: "main.(*Server).handle", want: "main.(*Server).handle"},
		{name: "folds a closure into the outer function", fn: "main.(*Server).handle.func1.2", want: "main.(*Server).handle"},
		{name: "folds a go statement wrapper", fn: "main.serve.gowrap1", want: "main.serve"},
		{name: "folds type parameters", fn: "main.Map[go.shape.int,go.shape.string]", want: "main.Map[...]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.IssueFunction(tt.fn))
		})
	}
}

func TestCauseOfWait(t *testing.T) {
	t.Parallel()
	lock := []domain.Frame{{Function: "sync.(*Mutex).Lock"}, {Function: "main.handle"}}
	wg := []domain.Frame{{Function: "sync.(*WaitGroup).Wait"}, {Function: "main.handle"}}
	tests := []struct {
		name   string
		reason string
		stack  []domain.Frame
		want   domain.MainCause
	}{
		{name: "tells a Lock wait from other sync waits by the stack", reason: "sync", stack: lock, want: domain.CauseLockWait},
		{name: "treats a WaitGroup wait as waiting for other goroutines", reason: "sync", stack: wg, want: domain.CauseSyncWait},
		{name: "treats the network as IO", reason: "network", want: domain.CauseIOWait},
		{name: "treats a sleep as SLEEP", reason: "sleep", want: domain.CauseSleep},
		{name: "treats a channel as waiting for other goroutines", reason: "chan receive", want: domain.CauseSyncWait},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.CauseOfWait(tt.reason, tt.stack))
		})
	}
}

func TestAgentOverlay(t *testing.T) {
	t.Parallel()
	dir := filepath.FromSlash("/app/cmd/api")
	main := domain.MainPackage{ImportPath: "example.com/app/cmd/api", Dir: dir, Files: []domain.SourceFile{
		{Path: filepath.Join(dir, "flags.go"), Data: []byte("package main\n\nvar port = 8080\n")},
		{Path: filepath.Join(dir, "main.go"), Data: []byte("package main // the entry\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(port)\n}\n")},
	}}
	agent := []domain.SourceFile{
		{Path: "agent.go", Data: []byte("package agent\n\nimport \"github.com/kakkky/gorviz/internal/app/agent/usecase\"\nimport \"github.com/kakkky/gorviz/internal/protocol/agentsocket\"\n")},
		{Path: "usecase/usecase_test.go", Data: []byte("package usecase_test\n")},
		{Path: "testhelper/mock/mock.go", Data: []byte("package mock\n")},
	}
	socket := []domain.SourceFile{{Path: "agentsocket.go", Data: []byte("package agentsocket\n")}}
	tests := []struct {
		name  string
		main  domain.MainPackage
		check func(t *testing.T, ov domain.Overlay, err error)
	}{
		{
			name: "adds the agent under the main package and rewrites its imports",
			main: main,
			check: func(t *testing.T, ov domain.Overlay, err error) {
				require.NoError(t, err)
				src := string(ov[filepath.Join(dir, "gorvizagent", "agent.go")])
				assert.Contains(t, src, `"example.com/app/cmd/api/gorvizagent/usecase"`)
				assert.Contains(t, src, `"example.com/app/cmd/api/gorvizagent/agentsocket"`)
				assert.Contains(t, ov, filepath.Join(dir, "gorvizagent", "agentsocket", "agentsocket.go"))
				assert.NotContains(t, ov, filepath.Join(dir, "gorvizagent", "usecase", "usecase_test.go"))
				assert.NotContains(t, ov, filepath.Join(dir, "gorvizagent", "testhelper", "mock", "mock.go"))
			},
		},
		{
			name: "stops the agent when main returns without moving any line of main",
			main: main,
			check: func(t *testing.T, ov domain.Overlay, err error) {
				require.NoError(t, err)
				src := string(ov[filepath.Join(dir, "main.go")])
				assert.Equal(t, 7, strings.Count(src, "\n"))
				assert.Contains(t, src, `package main; import gorvizagent "example.com/app/cmd/api/gorvizagent" // the entry`)
				assert.Contains(t, src, "func main() { defer gorvizagent.Stop();\n")
				assert.NotContains(t, ov, filepath.Join(dir, "flags.go"))
			},
		},
		{
			name: "rejects a package without func main",
			main: domain.MainPackage{ImportPath: "example.com/app/lib", Dir: dir, Files: main.Files[:1]},
			check: func(t *testing.T, _ domain.Overlay, err error) {
				require.ErrorIs(t, err, domain.ErrInvalidArgument)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ov, err := domain.AgentOverlay(tt.main, agent, socket)
			tt.check(t, ov, err)
		})
	}
}

func TestBuildCoverageReport(t *testing.T) {
	t.Parallel()
	profile := []byte(`mode: atomic
example.com/app/x/x.go:3.20,5.2 2 1
example.com/app/x/x.go:5.2,7.3 1 0
example.com/app/x/x.go:7.3,8.3 1 0
example.com/app/x/x.go:11.20,13.2 2 0
example.com/app/cmd/api/gorvizagent/agent.go:3.1,4.1 1 1
`)
	src := []byte("package x\n\nfunc A(v int) int {\n\tif v > 0 {\n\t\treturn 1\n\t}\n\treturn 2\n}\n\nfunc B() {\n\tprintln()\n\tprintln()\n}\n")
	tests := []struct {
		name  string
		check func(t *testing.T, r domain.CoverageReport)
	}{
		{
			name: "leaves out the agent",
			check: func(t *testing.T, r domain.CoverageReport) {
				assert.Equal(t, 6, r.Statements)
				assert.Equal(t, 2, r.Covered)
			},
		},
		{
			name: "groups the lines not reached by function as ranges",
			check: func(t *testing.T, r domain.CoverageReport) {
				require.Len(t, r.Partial, 1)
				assert.Equal(t, "example.com/app/x.A", r.Partial[0].Function)
				assert.Equal(t, []domain.LineRange{{From: 5, To: 8}}, r.Partial[0].Uncovered)
				assert.Equal(t, []string{"example.com/app/x.B"}, r.Unreached)
			},
		},
	}
	blocks, err := domain.ParseCoverageProfile(profile)
	require.NoError(t, err)
	r := domain.BuildCoverageReport(blocks, map[string][]byte{"example.com/app/x/x.go": src})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, r)
		})
	}
}
