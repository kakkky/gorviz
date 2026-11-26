package domain_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentOverlay(t *testing.T) {
	t.Parallel()
	dir := filepath.FromSlash("/app/cmd/api")
	main := domain.MainPackage{ImportPath: "example.com/app/cmd/api", Dir: dir, Files: []domain.SourceFile{
		{Path: filepath.Join(dir, "flags.go"), Data: []byte("package main\n\nvar port = 8080\n")},
		{Path: filepath.Join(dir, "main.go"), Data: []byte("package main // the entry\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(port)\n}\n")},
	}}
	agent := []domain.SourceFile{
		{Path: "agent.go", Data: []byte("package agent\n\nimport \"github.com/kakkky/gorviz/internal/app/agent/usecase\"\nimport \"github.com/kakkky/gorviz/internal/protocol/agent\"\n")},
		{Path: "usecase/usecase_test.go", Data: []byte("package usecase_test\n")},
		{Path: "testhelper/mock/mock.go", Data: []byte("package mock\n")},
	}
	protocol := []domain.SourceFile{{Path: "agent.go", Data: []byte("package agent\n")}, {Path: "agent_test.go", Data: []byte("package agent_test\n")}}
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
				assert.Contains(t, src, `"example.com/app/cmd/api/gorvizagent/protocol/agent"`)
				assert.Contains(t, ov, filepath.Join(dir, "gorvizagent", "protocol", "agent", "agent.go"))
				assert.NotContains(t, ov, filepath.Join(dir, "gorvizagent", "usecase", "usecase_test.go"))
				assert.NotContains(t, ov, filepath.Join(dir, "gorvizagent", "testhelper", "mock", "mock.go"))
				assert.NotContains(t, ov, filepath.Join(dir, "gorvizagent", "protocol", "agent", "agent_test.go"))
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
			ov, err := domain.AgentOverlay(tt.main, agent, protocol)
			tt.check(t, ov, err)
		})
	}
}
