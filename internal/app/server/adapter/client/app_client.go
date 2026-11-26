package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// AppClient はアプリとのやり取り (ビルド・子プロセスとしての起動・合図・負荷のリクエスト) を持つ
type AppClient struct {
	socketPath    string
	buildDir      string
	transport     *http.Transport
	agentFiles    map[string][]byte
	protocolFiles map[string][]byte
}

// NewAppClient の agent・protocol は、ビルドに差し込む agent と agent との決まりのファイル (根からのパスと中身)
func NewAppClient(socketPath, buildDir string, agent, protocol map[string][]byte) (*AppClient, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns = 0
	tr.MaxIdleConnsPerHost = 1024
	tr.MaxConnsPerHost = 0
	return &AppClient{socketPath: socketPath, buildDir: buildDir, transport: tr, agentFiles: agent, protocolFiles: protocol}, nil
}

func (c *AppClient) AgentFiles() ([]domain.SourceFile, []domain.SourceFile) {
	return sourceFiles(c.agentFiles), sourceFiles(c.protocolFiles)
}

func sourceFiles(files map[string][]byte) []domain.SourceFile {
	out := make([]domain.SourceFile, 0, len(files))
	for _, p := range slices.Sorted(maps.Keys(files)) {
		out = append(out, domain.SourceFile{Path: p, Data: files[p]})
	}
	return out
}

type goListPackage struct {
	ImportPath string
	Dir        string
	Name       string
	GoFiles    []string
	Module     *struct {
		Path string
		Dir  string
	}
}

func goCommand(ctx context.Context, in domain.BuildAppInput, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = in.Dir
	cmd.Env = in.Env
	return cmd
}

func (c *AppClient) LoadMainPackage(ctx context.Context, in domain.BuildAppInput) (domain.MainPackage, domain.AppBuild, error) {
	var stderr bytes.Buffer
	cmd := goCommand(ctx, in, "list", "-json=ImportPath,Dir,Name,GoFiles,Module", in.Package)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return domain.MainPackage{}, domain.AppBuild{}, domain.ErrInvalidArgument.With("go list " + in.Package + ": " + strings.TrimSpace(stderr.String()))
	}
	var p goListPackage
	if err := json.Unmarshal(out, &p); err != nil {
		return domain.MainPackage{}, domain.AppBuild{}, domain.ErrInvalidArgument.Wrap(err, "pass one main package, like gorviz run ./cmd/api")
	}
	if p.Name != "main" {
		return domain.MainPackage{}, domain.AppBuild{}, domain.ErrInvalidArgument.With(in.Package + " is package " + p.Name + ", not a main package")
	}
	if p.Module == nil {
		return domain.MainPackage{}, domain.AppBuild{}, domain.ErrInvalidArgument.With(in.Package + " is not in a Go module")
	}
	main := domain.MainPackage{ImportPath: p.ImportPath, Dir: p.Dir}
	for _, f := range p.GoFiles {
		path := filepath.Join(p.Dir, f)
		b, err := os.ReadFile(path)
		if err != nil {
			return domain.MainPackage{}, domain.AppBuild{}, domain.ErrInternal.Wrap(err, "read main package")
		}
		main.Files = append(main.Files, domain.SourceFile{Path: path, Data: b})
	}
	stderr.Reset()
	deps := goCommand(ctx, in, "list", "-deps", "-f", `{{if .Module}}{{if .Module.Main}}{{.ImportPath}}{{end}}{{end}}`, in.Package)
	deps.Stderr = &stderr
	out, err = deps.Output()
	if err != nil {
		return domain.MainPackage{}, domain.AppBuild{}, domain.ErrInvalidArgument.With("go list -deps " + in.Package + ": " + strings.TrimSpace(stderr.String()))
	}
	build := domain.AppBuild{WorkDir: in.Dir, Binary: filepath.Join(c.buildDir, "app"), ModulePath: p.Module.Path, ModuleDir: p.Module.Dir, MainPackage: p.ImportPath, MainDir: p.Dir}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			build.CoverPackages = append(build.CoverPackages, l)
		}
	}
	return main, build, nil
}

// BuildApp は overlay で agent を差し込み、coverage を取れるようにビルドする。
// go build -cover は cover ツールに overlay する前のファイルを渡すので、-toolexec で cover ツールの入力だけを overlay したファイルに差し替える
func (c *AppClient) BuildApp(ctx context.Context, in domain.BuildAppInput, build domain.AppBuild, overlay domain.Overlay) error {
	dir := filepath.Join(c.buildDir, "overlay")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return domain.ErrInternal.Wrap(err, "make overlay dir")
	}
	replace := map[string]string{}
	for path, data := range overlay {
		file := filepath.Join(dir, strings.ReplaceAll(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator), "_"))
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return domain.ErrInternal.Wrap(err, "write overlay file")
		}
		replace[path] = file
	}
	b, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return domain.ErrInternal.Wrap(err, "encode overlay")
	}
	overlayFile := filepath.Join(c.buildDir, "overlay.json")
	if err := os.WriteFile(overlayFile, b, 0o644); err != nil {
		return domain.ErrInternal.Wrap(err, "write overlay")
	}
	args := []string{"build", "-overlay", overlayFile, "-o", build.Binary}
	if len(build.CoverPackages) > 0 {
		toolexec, err := c.writeCoverToolexec(replace)
		if err != nil {
			return err
		}
		args = append(args, "-toolexec", toolexec, "-cover", "-covermode=atomic", "-coverpkg="+strings.Join(build.CoverPackages, ","))
	}
	args = append(args, in.Package)
	cmd := goCommand(ctx, in, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return domain.ErrInvalidArgument.With(strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writeCoverToolexec は、cover ツールに渡るファイルのうち overlay したものを差し替えて、ツールをそのまま呼ぶスクリプトを書く
func (c *AppClient) writeCoverToolexec(replace map[string]string) (string, error) {
	var cases strings.Builder
	for from, to := range replace {
		cases.WriteString("    " + shellQuote(from) + ") a=" + shellQuote(to) + " ;;\n")
	}
	script := "#!/bin/sh\ntool=\"$1\"; shift\ncase \"$(basename \"$tool\")\" in\ncover|cover.exe)\n  n=$#\n  while [ $n -gt 0 ]; do\n    a=\"$1\"; shift\n    case \"$a\" in\n" + cases.String() + "    esac\n    set -- \"$@\" \"$a\"\n    n=$((n-1))\n  done\n  ;;\nesac\nexec \"$tool\" \"$@\"\n"
	path := filepath.Join(c.buildDir, "toolexec.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return "", domain.ErrInternal.Wrap(err, "write the toolexec script")
	}
	return path, nil
}

type appProcess struct {
	cmd *exec.Cmd
}

func (c *AppClient) StartApp(_ context.Context, build domain.AppBuild, args []string, env []string) (domain.AppProcess, error) {
	cmd := exec.Command(build.Binary, args...)
	cmd.Dir = build.WorkDir
	cmd.Env = append(append([]string{}, env...), agent.EnvSocket+"="+c.socketPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, domain.ErrInvalidArgument.Wrap(err, "start the app")
	}
	return &appProcess{cmd: cmd}, nil
}

func (p *appProcess) Terminate() error {
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return domain.ErrInternal.Wrap(err, "send SIGTERM to the app")
	}
	return nil
}

func (p *appProcess) Kill() error {
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return domain.ErrInternal.Wrap(err, "kill the app")
	}
	return nil
}

func (p *appProcess) Wait() int {
	_ = p.cmd.Wait()
	return p.cmd.ProcessState.ExitCode()
}

type loadClient struct {
	http *http.Client
}

func (c *AppClient) NewLoadClient() domain.LoadClient {
	jar, _ := cookiejar.New(nil)
	return &loadClient{http: &http.Client{
		Transport:     c.transport,
		Jar:           jar,
		Timeout:       domain.RequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

const maxResponseBody = 1 << 20

func (l *loadClient) Send(ctx context.Context, r domain.Request) domain.Response {
	if r.Cancel > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Cancel)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, bytes.NewReader(r.Body))
	if err != nil {
		return domain.Response{Err: err.Error()}
	}
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := l.http.Do(req)
	if err != nil {
		return domain.Response{Err: err.Error(), Latency: time.Since(start), Canceled: r.Cancel > 0 && ctx.Err() != nil}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	out := domain.Response{Status: resp.StatusCode, Header: resp.Header, Body: body, Latency: time.Since(start)}
	if err != nil {
		out.Err = err.Error()
		out.Canceled = r.Cancel > 0 && ctx.Err() != nil
	}
	return out
}
