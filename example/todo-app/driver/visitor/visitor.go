// Package visitor は裏で動く仮想の訪問者。アプリの外から HTTP で一覧を見て、ときどき作り、未完了のものを完了にする。
// リマインダー付きでは作らないので、GOROUTINE_LEAK は画面から作ったときだけ起きる
package visitor

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kakkky/scope"
)

var completeLink = regexp.MustCompile(`/todos/(\d+)/complete`)

type Visitors struct {
	base   string
	n      int
	client *http.Client
}

func NewVisitors(addr string, n int) *Visitors {
	return &Visitors{
		base: "http://" + addr,
		n:    n,
		client: &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (v *Visitors) Run(ctx context.Context) error {
	return scope.Run(ctx, func(sc *scope.Scope) error {
		for i := range v.n {
			sc.Go(func(ctx context.Context) error {
				v.visit(ctx, i)
				return nil
			})
		}
		return nil
	})
}

func (v *Visitors) visit(ctx context.Context, n int) {
	if !sleep(ctx, time.Second) {
		return
	}
	for ctx.Err() == nil {
		page, err := v.get(ctx, v.base+"/")
		if err != nil {
			if !sleep(ctx, time.Second) {
				return
			}
			continue
		}
		if rand.IntN(4) == 0 {
			v.post(ctx, v.base+"/todos", url.Values{"title": {fmt.Sprintf("visit #%d (visitor %d)", rand.IntN(1000), n)}})
		} else if m := completeLink.FindStringSubmatch(page); m != nil {
			v.post(ctx, v.base+"/todos/"+m[1]+"/complete", nil)
		}
		if !sleep(ctx, time.Duration(300+rand.IntN(900))*time.Millisecond) {
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (v *Visitors) get(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	res, err := v.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return string(b), err
}

func (v *Visitors) post(ctx context.Context, u string, form url.Values) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if res, err := v.client.Do(req); err == nil {
		res.Body.Close()
	}
}
