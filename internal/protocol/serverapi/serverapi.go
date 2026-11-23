// Package serverapi は server の HTTP API の契約。server は Routes のパスで待ち受け、cli と mcp は Client で呼ぶ。応答は解釈せず、そのまま返す
package serverapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	MediaTypeTOON = "application/toon"
	MediaTypeJSON = "application/json"

	// TokenEnv は cli と mcp がトークンを読む環境変数
	TokenEnv = "GORVIZ_TOKEN"
)

// Route は 1 つの操作。Pattern は net/http.ServeMux の形。Params はパスの {name} と query の引数
type Route struct {
	Name    string
	Pattern string
	Params  []Param
}

type ParamIn string

const (
	InPath  ParamIn = "path"
	InQuery ParamIn = "query"
)

// Param の Repeated は同じ名前で複数渡せる (attr=key=value など)
type Param struct {
	Name     string
	In       ParamIn
	Required bool
	Repeated bool
}

var (
	pService   = Param{Name: "service", In: InPath, Required: true}
	pInstance  = Param{Name: "instance", In: InPath, Required: true}
	pGoroutine = Param{Name: "goroutine", In: InPath, Required: true}
	pFrom      = Param{Name: "from", In: InQuery}
	pTo        = Param{Name: "to", In: InQuery}
	pCursor    = Param{Name: "cursor", In: InQuery}
	pMaxBytes  = Param{Name: "max-bytes", In: InQuery}
	pLevel     = Param{Name: "level", In: InQuery}
	pInstanceQ = Param{Name: "instance", In: InQuery}
	pAttr      = Param{Name: "attr", In: InQuery, Repeated: true}
	pLog       = Param{Name: "log", In: InQuery}
	pIssue     = Param{Name: "issue", In: InQuery}
	pOccurred  = Param{Name: "occurrence", In: InQuery}
	pWokenFrom = Param{Name: "woken", In: InQuery}
)

var (
	ListServices        = Route{Name: "list-services", Pattern: "GET /api/v1/services"}
	GetOverview         = Route{Name: "get-overview", Pattern: "GET /api/v1/services/{service}/overview", Params: []Param{pService, pFrom, pTo}}
	ListIssues          = Route{Name: "list-issues", Pattern: "GET /api/v1/services/{service}/issues", Params: []Param{pService, pFrom, pTo, pCursor, pMaxBytes}}
	GetIssue            = Route{Name: "get-issue", Pattern: "GET /api/v1/services/{service}/issues/{issue}", Params: []Param{pService, {Name: "issue", In: InPath, Required: true}, pFrom, pTo}}
	SearchLogs          = Route{Name: "search-logs", Pattern: "GET /api/v1/services/{service}/logs", Params: []Param{pService, pFrom, pTo, pLevel, pInstanceQ, pAttr, {Name: "q", In: InQuery}, pCursor, pMaxBytes}}
	CountLogs           = Route{Name: "count-logs", Pattern: "GET /api/v1/services/{service}/log-counts", Params: []Param{pService, pFrom, pTo, pLevel, pInstanceQ, pAttr, {Name: "by", In: InQuery, Required: true}}}
	GetLog              = Route{Name: "get-log", Pattern: "GET /api/v1/services/{service}/logs/{log}", Params: []Param{pService, {Name: "log", In: InPath, Required: true}}}
	DiagnoseGoroutine   = Route{Name: "diagnose-goroutine", Pattern: "GET /api/v1/services/{service}/instances/{instance}/goroutines/{goroutine}/diagnosis", Params: []Param{pService, pInstance, pGoroutine, pFrom, pTo, pLog, pIssue, pOccurred, pWokenFrom}}
	ListStateChanges    = Route{Name: "list-goroutine-state-changes", Pattern: "GET /api/v1/services/{service}/instances/{instance}/goroutines/{goroutine}/state-changes", Params: []Param{pService, pInstance, pGoroutine, pFrom, pTo, pLog, pIssue, pOccurred, pWokenFrom, pCursor, pMaxBytes}}
	SummarizeGoroutines = Route{Name: "summarize-goroutines", Pattern: "GET /api/v1/services/{service}/instances/{instance}/goroutine-summary", Params: []Param{pService, pInstance, pFrom, pTo, pMaxBytes}}
	ListGoroutinesAt    = Route{Name: "list-goroutines-at", Pattern: "GET /api/v1/services/{service}/instances/{instance}/goroutines", Params: []Param{pService, pInstance, {Name: "at", In: InQuery, Required: true}, pCursor, pMaxBytes}}
	ListAvailableData   = Route{Name: "list-available-data", Pattern: "GET /api/v1/services/{service}/instances/{instance}/available-data", Params: []Param{pService, pInstance, pFrom, pTo}}
	AllRoutes           = []Route{ListServices, GetOverview, ListIssues, GetIssue, SearchLogs, CountLogs, GetLog, DiagnoseGoroutine, ListStateChanges, SummarizeGoroutines, ListGoroutinesAt, ListAvailableData}
)

// Query の期間は from・to (RFC 3339)、続きは cursor、返す量の上限は max-bytes。
// 調べる期間を入口で決めるときは、from・to の代わりに log (ログの ID)、issue と occurrence (発生の時刻)、woken (起こされた側の goroutine) のどれかを渡す
type Query = url.Values

// Path は Pattern の {name} を params で埋める
func (r Route) Path(params map[string]string) string {
	_, p, _ := strings.Cut(r.Pattern, " ")
	for k, v := range params {
		p = strings.ReplaceAll(p, "{"+k+"}", url.PathEscape(v))
	}
	return p
}

var (
	ErrUnauthorized = errors.New("the gorviz server rejected the token")
	ErrNotFound     = errors.New("not found")
	ErrBadRequest   = errors.New("bad request")
)

// ResponseError は server がエラーで返した中身を、そのまま持つ
type ResponseError struct {
	Kind error
	Body []byte
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("%v: %s", e.Kind, strings.TrimSpace(string(e.Body)))
}

func (e *ResponseError) Unwrap() error { return e.Kind }

type Client struct {
	base   string
	token  string
	accept string
	http   *http.Client
}

// NewClient の accept は MediaTypeTOON か MediaTypeJSON
func NewClient(base, token, accept string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{base: strings.TrimSuffix(base, "/"), token: token, accept: accept, http: hc}
}

// Call は route を呼び、応答の本文をそのまま返す
func (c *Client) Call(ctx context.Context, route Route, params map[string]string, q Query) ([]byte, error) {
	u := c.base + route.Path(params)
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", c.accept)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, &ResponseError{Kind: ErrUnauthorized, Body: body}
	case res.StatusCode == http.StatusNotFound:
		return nil, &ResponseError{Kind: ErrNotFound, Body: body}
	case res.StatusCode >= 400 && res.StatusCode < 500:
		return nil, &ResponseError{Kind: ErrBadRequest, Body: body}
	case res.StatusCode >= 500:
		return nil, &ResponseError{Kind: fmt.Errorf("server error %d", res.StatusCode), Body: body}
	}
	return body, nil
}
