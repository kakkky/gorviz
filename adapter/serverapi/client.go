// Package serverapi は server の JSON の API を呼ぶクライアント。TUI・CLI・MCP は server とだけ話す
package serverapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/api"
	"github.com/kakkky/gorviz/domain"
)

type Client struct {
	base  string
	agent string
	http  *http.Client
}

// NewClient の agent が空なら、server が最初につながった agent を使う
func NewClient(base, agent string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), agent: agent, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Agents(ctx context.Context) ([]api.AgentJSON, error) {
	var out []api.AgentJSON
	return out, c.get(ctx, "/api/agents", nil, &out)
}

func (c *Client) Overview(ctx context.Context) (api.OverviewJSON, error) {
	var out api.OverviewJSON
	return out, c.get(ctx, "/api/overview", nil, &out)
}

func (c *Client) Scheduler(ctx context.Context) (api.SchedulerJSON, error) {
	var out api.SchedulerJSON
	return out, c.get(ctx, "/api/scheduler", nil, &out)
}

func (c *Client) Issues(ctx context.Context) ([]api.IssueJSON, error) {
	var out []api.IssueJSON
	return out, c.get(ctx, "/api/issues", nil, &out)
}

type LogQuery struct {
	Query string
	Level string
	From  time.Time
	To    time.Time
	Limit int
}

func (c *Client) Logs(ctx context.Context, q LogQuery) ([]api.LogJSON, error) {
	v := url.Values{"q": {q.Query}, "level": {q.Level}}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if !q.From.IsZero() {
		v.Set("from", strconv.FormatInt(q.From.UnixNano(), 10))
	}
	if !q.To.IsZero() {
		v.Set("to", strconv.FormatInt(q.To.UnixNano(), 10))
	}
	var out []api.LogJSON
	return out, c.get(ctx, "/api/logs", v, &out)
}

// Goroutine は G を at の前後で診断する。from を渡すと [from, at] の区間 (リクエストの区間など) を見る
func (c *Client) Goroutine(ctx context.Context, id int64, at, from time.Time) (api.GoroutineJSON, error) {
	v := url.Values{"at": {strconv.FormatInt(at.UnixNano(), 10)}}
	if !from.IsZero() {
		v.Set("from", strconv.FormatInt(from.UnixNano(), 10))
	}
	var out api.GoroutineJSON
	return out, c.get(ctx, fmt.Sprintf("/api/goroutines/%d", id), v, &out)
}

func (c *Client) Recordings(ctx context.Context) ([]domain.SavedRecording, error) {
	var out []domain.SavedRecording
	return out, c.get(ctx, "/api/recordings", nil, &out)
}

// RecordingChunk は保存した録画の n 番目の区切りを、取ったままの trace として返す
func (c *Client) RecordingChunk(ctx context.Context, id string, n int) ([]byte, error) {
	body, err := c.do(ctx, fmt.Sprintf("/api/recordings/%s/chunks/%d", url.PathEscape(id), n), nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(body)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	body, err := c.do(ctx, path, q)
	if err != nil {
		return err
	}
	defer body.Close()
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return domain.ErrInternal.Wrap(err, "decode "+path)
	}
	return nil
}

func (c *Client) do(ctx context.Context, path string, q url.Values) (io.ReadCloser, error) {
	if q == nil {
		q = url.Values{}
	}
	if c.agent != "" {
		q.Set("agent", c.agent)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, domain.ErrInvalidArgument.Wrap(err, "build a request to the server")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, domain.ErrUnavailable.Wrap(err, "request "+path)
	}
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&e)
		return nil, domain.ErrUnavailable.With(fmt.Sprintf("%s returned %d: %s", path, res.StatusCode, e.Error))
	}
	return res.Body, nil
}
