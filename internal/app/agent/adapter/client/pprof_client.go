package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// maxProfileBytes は trace 以外のプロファイルを読む上限。件数とスタックだけなので、これを超えるのは相手がおかしいとき
const maxProfileBytes = 16 << 20

// PprofClient はアプリが公開する net/http/pprof から取る (sidecar 型・daemon 型)
type PprofClient struct {
	http    *http.Client
	timeout time.Duration
}

func NewPprofClient(cfg *config.Config) (*PprofClient, error) {
	return &PprofClient{http: &http.Client{}, timeout: cfg.RequestTimeout}, nil
}

// FetchExecutionTrace は length の間 trace を取る。pprof は取り終えてから本文を返すので、読み切った時刻をチャンクの終わりの目安にする
func (c *PprofClient) FetchExecutionTrace(ctx context.Context, t domain.TargetInstance, length time.Duration) (domain.RawTrace, error) {
	// net/http/pprof の Trace ハンドラは seconds を小数で受け付ける
	seconds := strconv.FormatFloat(max(length.Seconds(), 0.05), 'f', -1, 64)
	body, err := c.get(ctx, t, "/debug/pprof/trace", url.Values{"seconds": {seconds}}, length+c.timeout, -1)
	if err != nil {
		return domain.RawTrace{}, err
	}
	return domain.RawTrace{Data: body, ReceivedAt: time.Now()}, nil
}

func (c *PprofClient) FetchThreadCreateProfile(ctx context.Context, t domain.TargetInstance) ([]byte, error) {
	return c.get(ctx, t, "/debug/pprof/threadcreate", url.Values{"debug": {"1"}}, c.timeout, maxProfileBytes)
}

// FetchGoroutineLeakProfile はリークした goroutine だけを、スタックごとの件数 (debug=1) で取る。debug=2 は書き出す間ずっと STW になるので使わない
func (c *PprofClient) FetchGoroutineLeakProfile(ctx context.Context, t domain.TargetInstance) ([]byte, error) {
	return c.get(ctx, t, "/debug/pprof/goroutineleak", url.Values{"debug": {"1"}}, c.timeout, maxProfileBytes)
}

func (c *PprofClient) get(ctx context.Context, t domain.TargetInstance, path string, query url.Values, timeout time.Duration, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u := "http://" + t.PprofAddr + path + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, domain.ErrInvalidArgument.Wrap(err, "build pprof request")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "request "+path)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, domain.ErrNotFound.With(path + " is not served by the app")
	}
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, domain.ErrInternal.With(fmt.Sprintf("%s returned %d: %s", path, res.StatusCode, strings.TrimSpace(string(msg))))
	}
	var r io.Reader = res.Body
	if limit > 0 {
		r = io.LimitReader(res.Body, limit)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read "+path)
	}
	return body, nil
}
