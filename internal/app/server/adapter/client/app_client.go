package client

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// AppClient はアプリとのやり取り (ビルド・子プロセスとしての起動・合図・pprof からの取得・負荷のリクエスト) を持つ
type AppClient struct {
	pprofSocket string
	buildDir    string
	transport   *http.Transport
	pprof       *http.Client
}

// NewAppClient の pprofSocket は、アプリに pprof を開かせる unix socket
func NewAppClient(pprofSocket, buildDir string) (*AppClient, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns = 0
	tr.MaxIdleConnsPerHost = 1024
	tr.MaxConnsPerHost = 0
	pprof := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", pprofSocket)
	}}}
	return &AppClient{pprofSocket: pprofSocket, buildDir: buildDir, transport: tr, pprof: pprof}, nil
}

type goListPackage struct {
	ImportPath string
	Dir        string
	Name       string
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
	cmd := goCommand(ctx, in, "list", "-json=ImportPath,Dir,Name,Module", in.Package)
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
	build := domain.AppBuild{WorkDir: in.Dir, Binary: filepath.Join(c.buildDir, "app"), ModulePath: p.Module.Path, ModuleDir: p.Module.Dir, MainPackage: p.ImportPath, MainDir: p.Dir}
	return main, build, nil
}

// BuildApp は overlay で pprof を開く処理を差し込んでビルドする
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
	cmd := goCommand(ctx, in, "build", "-overlay", overlayFile, "-o", build.Binary, in.Package)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return domain.ErrInvalidArgument.With(strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *AppClient) GoVersion(build domain.AppBuild) (string, error) {
	info, err := buildinfo.ReadFile(build.Binary)
	if err != nil {
		return "", domain.ErrInternal.Wrap(err, "read the build info of the app")
	}
	return info.GoVersion, nil
}

type appProcess struct {
	cmd *exec.Cmd
}

func (c *AppClient) StartApp(_ context.Context, build domain.AppBuild, args []string, env []string) (domain.AppProcess, error) {
	cmd := exec.Command(build.Binary, args...)
	cmd.Dir = build.WorkDir
	_ = os.Remove(c.pprofSocket)
	cmd.Env = append(append([]string{}, env...), domain.PprofSocketEnv+"="+c.pprofSocket)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, domain.ErrInvalidArgument.Wrap(err, "start the app")
	}
	return &appProcess{cmd: cmd}, nil
}

func (p *appProcess) PID() int { return p.cmd.Process.Pid }

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

// maxProfileBytes は trace 以外のプロファイルを読む上限。件数とスタックだけなので、これを超えるのは相手がおかしいとき
const maxProfileBytes = 16 << 20

// FetchTrace は length の間 trace を取る。pprof は取り終えてから本文を書き切るので、アプリが途中で終われば取れない
func (c *AppClient) FetchTrace(ctx context.Context, length time.Duration) ([]byte, error) {
	seconds := strconv.FormatFloat(length.Seconds(), 'f', -1, 64)
	return c.fetchProfile(ctx, "/debug/pprof/trace?seconds="+seconds, length+5*time.Second, -1)
}

func (c *AppClient) FetchThreadCreateProfile(ctx context.Context) ([]byte, error) {
	return c.fetchProfile(ctx, "/debug/pprof/threadcreate?debug=1", 5*time.Second, maxProfileBytes)
}

// FetchGoroutineLeakProfile はリークした goroutine だけを、スタックごとの件数 (debug=1) で取る
func (c *AppClient) FetchGoroutineLeakProfile(ctx context.Context) ([]byte, error) {
	return c.fetchProfile(ctx, "/debug/pprof/goroutineleak?debug=1", 10*time.Second, maxProfileBytes)
}

func (c *AppClient) fetchProfile(ctx context.Context, path string, timeout time.Duration, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://app"+path, nil)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "build the pprof request")
	}
	resp, err := c.pprof.Do(req)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "request "+path)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, domain.ErrNotFound.With(path + " is not served by the app")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, domain.ErrInternal.With(path + " returned " + resp.Status)
	}
	var r io.Reader = resp.Body
	if limit > 0 {
		r = io.LimitReader(resp.Body, limit)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read "+path)
	}
	return body, nil
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
