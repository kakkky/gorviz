package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
)

type RuntimeClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewRuntimeClient(cfg *config.Config) *RuntimeClient {
	return &RuntimeClient{
		baseURL:    strings.TrimRight(cfg.TargetURL, "/"),
		httpClient: &http.Client{Timeout: cfg.HTTPTimeout + cfg.TraceWindow},
	}
}

func (c *RuntimeClient) FetchGoroutines(ctx context.Context) ([]domain.Goroutine, error) {
	body, err := c.get(ctx, "/debug/pprof/goroutine", url.Values{"debug": {"2"}})
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return parseGoroutineDump(body)
}

func (c *RuntimeClient) FetchGoroutinesWithLeakCheck(ctx context.Context) ([]domain.Goroutine, error) {
	body, err := c.get(ctx, "/debug/pprof/goroutineleak", url.Values{"debug": {"2"}})
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return parseGoroutineDump(body)
}

func (c *RuntimeClient) FetchTraceChunk(ctx context.Context, duration time.Duration) (domain.TraceChunk, error) {
	// pprof の seconds は float で受け付ける (net/http/pprof の Trace ハンドラは ParseFloat で読む)
	seconds := fmt.Sprintf("%g", math.Max(duration.Seconds(), 0.05))
	body, err := c.get(ctx, "/debug/pprof/trace", url.Values{"seconds": {seconds}})
	if err != nil {
		return domain.TraceChunk{}, err
	}
	defer body.Close()
	// trace は取り終えてから body が流れてくるので、読み切った時刻を窓の終わりの目安にする
	data, err := io.ReadAll(body)
	if err != nil {
		return domain.TraceChunk{}, domain.ErrUnavailable.Wrap(err, "read execution trace")
	}
	return buildTraceChunk(bytes.NewReader(data), time.Now())
}

func (c *RuntimeClient) get(ctx context.Context, path string, query url.Values) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, domain.ErrInvalidArgument.Wrap(err, "build request to target")
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, domain.ErrUnavailable.Wrap(err, "request "+path)
	}
	if res.StatusCode == http.StatusNotFound {
		res.Body.Close()
		return nil, domain.ErrNotFound.With(path + " is not served by the target")
	}
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		res.Body.Close()
		return nil, domain.ErrUnavailable.With(fmt.Sprintf("%s returned %d: %s", path, res.StatusCode, strings.TrimSpace(string(msg))))
	}
	return res.Body, nil
}
