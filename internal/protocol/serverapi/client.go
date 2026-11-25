package serverapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client は server の HTTP API を呼ぶ。応答は解釈せず、そのまま返す
type Client struct {
	base string
	http *http.Client
}

func NewClient(addr string) *Client {
	return &Client{base: "http://" + addr, http: &http.Client{}}
}

// Response は応答の中身と、server が載せた終了コード。中身を読み切ってから ExitCode を呼ぶ (trailer で届くことがあるため)
type Response struct {
	Body io.ReadCloser
	resp *http.Response
}

func (r *Response) ExitCode() int {
	v := r.resp.Trailer.Get(HeaderExitCode)
	if v == "" {
		v = r.resp.Header.Get(HeaderExitCode)
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

// Do は method と path (ルートの {id} は埋めたもの) で呼ぶ。json が true なら JSON で、そうでなければ TOON で返させる
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body []byte, json bool) (*Response, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	accept := MediaTOON
	if json {
		accept = MediaJSON
	}
	req.Header.Set("Accept", accept)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	return &Response{Body: resp.Body, resp: resp}, nil
}

func PathOf(route, id string) string {
	return strings.Replace(route, "{id}", url.PathEscape(id), 1)
}
