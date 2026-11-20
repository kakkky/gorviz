// Package serverapi は server の JSON の API。形 (型) は server の handler と CLI・MCP が共有し、Client はその API を呼ぶ。CLI・MCP は server とだけ話す
package serverapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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

func (c *Client) Agents(ctx context.Context) ([]AgentJSON, error) {
	var out []AgentJSON
	return out, c.get(ctx, "/api/agents", nil, &out)
}

func (c *Client) Overview(ctx context.Context) (OverviewJSON, error) {
	var out OverviewJSON
	return out, c.get(ctx, "/api/overview", nil, &out)
}

func (c *Client) Issues(ctx context.Context) ([]IssueJSON, error) {
	var out []IssueJSON
	return out, c.get(ctx, "/api/issues", nil, &out)
}

func (c *Client) Logs(ctx context.Context, q LogQuery) ([]LogJSON, error) {
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
	var out []LogJSON
	return out, c.get(ctx, "/api/logs", v, &out)
}

// Goroutine は G を at の前後で診断する。from を渡すと [from, at] の区間 (リクエストの区間など) を見る
func (c *Client) Goroutine(ctx context.Context, id int64, at, from time.Time) (GoroutineJSON, error) {
	v := url.Values{"at": {strconv.FormatInt(at.UnixNano(), 10)}}
	if !from.IsZero() {
		v.Set("from", strconv.FormatInt(from.UnixNano(), 10))
	}
	var out GoroutineJSON
	return out, c.get(ctx, fmt.Sprintf("/api/goroutines/%d", id), v, &out)
}

func (c *Client) Recordings(ctx context.Context) ([]model.SavedRecording, error) {
	var out []model.SavedRecording
	return out, c.get(ctx, "/api/recordings", nil, &out)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	body, err := c.do(ctx, path, q)
	if err != nil {
		return err
	}
	defer body.Close()
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return model.ErrInternal.Wrap(err, "decode "+path)
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
		return nil, model.ErrInvalidArgument.Wrap(err, "build a request to the server")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, model.ErrUnavailable.Wrap(err, "request "+path)
	}
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&e)
		return nil, model.ErrUnavailable.With(fmt.Sprintf("%s returned %d: %s", path, res.StatusCode, e.Error))
	}
	return res.Body, nil
}
