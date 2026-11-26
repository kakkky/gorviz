package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/kakkky/gorviz/internal/protocol/server"
)

// ServerClient は server を起動し、その HTTP API を unix socket 越しに呼ぶ。応答は解釈せず、そのまま返す
type ServerClient struct{}

func NewServerClient() (*ServerClient, error) {
	return &ServerClient{}, nil
}

func httpClient(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}}
}

// Response は応答の中身と、server が載せた終了コード。中身を読み切ってから ExitCode を呼ぶ (trailer で届くことがあるため)
type Response struct {
	Body io.ReadCloser
	resp *http.Response
}

func (r *Response) ExitCode() int {
	v := r.resp.Trailer.Get(server.HeaderExitCode)
	if v == "" {
		v = r.resp.Header.Get(server.HeaderExitCode)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		if r.resp.StatusCode >= 400 {
			return 2
		}
		return 0
	}
	return n
}

// Do は socket で待ち受ける server を method と path (エンドポイントの {id} は埋めたもの) で呼ぶ。json が true なら JSON で、そうでなければ TOON で返させる
func (c *ServerClient) Do(ctx context.Context, socket, method, path string, query url.Values, body []byte, json bool) (*Response, error) {
	u := "http://gorviz" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	accept := server.MediaTOON
	if json {
		accept = server.MediaJSON
	}
	req.Header.Set("Accept", accept)
	resp, err := httpClient(socket).Do(req)
	if err != nil {
		return nil, err
	}
	return &Response{Body: resp.Body, resp: resp}, nil
}

// StartServer は今の実行ファイルを server として、端末から切り離して起動する。環境変数は gorviz run のものをそのまま渡す
func (c *ServerClient) StartServer(socket, dir, pkg string, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(server.LogPath()), 0o755); err != nil {
		return err
	}
	log, err := os.OpenFile(server.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(exe)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), server.EnvServer+"=1", server.EnvSocket+"="+socket, server.EnvAppDir+"="+dir, server.EnvAppPackage+"="+pkg, server.EnvAppArgs+"="+string(argsJSON))
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
