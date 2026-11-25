package client

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// ToolchainClient は Go のツールチェーンで coverage を text 形式にし、アプリのソースを読む
type ToolchainClient struct {
	workDir string
}

func NewToolchainClient(workDir string) (*ToolchainClient, error) {
	return &ToolchainClient{workDir: workDir}, nil
}

func (c *ToolchainClient) CoverageProfile(ctx context.Context, files []domain.CoverageFile) ([]byte, error) {
	if len(files) == 0 {
		return nil, domain.ErrNotFound.With("the app was built without packages to cover")
	}
	dir, err := os.MkdirTemp(c.workDir, "coverage-")
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "make coverage dir")
	}
	defer os.RemoveAll(dir)
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(f.Name)), f.Data, 0o644); err != nil {
			return nil, domain.ErrInternal.Wrap(err, "write coverage file")
		}
	}
	out := filepath.Join(dir, "profile.txt")
	cmd := exec.CommandContext(ctx, "go", "tool", "covdata", "textfmt", "-i="+dir, "-o="+out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, domain.ErrInternal.With("go tool covdata: " + strings.TrimSpace(string(b)))
	}
	b, err := os.ReadFile(out)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read coverage profile")
	}
	return b, nil
}

func (c *ToolchainClient) ReadAppSources(build domain.AppBuild, files []string) map[string][]byte {
	out := map[string][]byte{}
	for _, f := range files {
		rel, ok := strings.CutPrefix(f, build.ModulePath+"/")
		if !ok {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(build.ModuleDir, filepath.FromSlash(rel))); err == nil {
			out[f] = b
		}
	}
	return out
}
