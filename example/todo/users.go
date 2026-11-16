package main

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
)

var todoIDPattern = regexp.MustCompile(`/todos/(\d+)/complete`)

// simulateUser は裏で動く仮想ユーザー。一覧を見て、ときどき作り、未完了のものを完了にする。
// リマインダー付きでは作らないので、LEAK は画面から作ったときだけ起きる
func simulateUser(ctx context.Context, base string, n int) {
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	time.Sleep(time.Second)
	for i := 0; ctx.Err() == nil; i++ {
		page, err := get(ctx, client, base+"/")
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		switch {
		case rand.IntN(4) == 0:
			form := url.Values{"title": {fmt.Sprintf("%s (user %d)", sampleTitles[rand.IntN(len(sampleTitles))], n)}}
			post(ctx, client, base+"/todos", form)
		default:
			if m := todoIDPattern.FindStringSubmatch(page); m != nil {
				post(ctx, client, base+"/todos/"+m[1]+"/complete", nil)
			}
		}
		time.Sleep(time.Duration(300+rand.IntN(900)) * time.Millisecond)
	}
}

func get(ctx context.Context, c *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return string(b), err
}

func post(ctx context.Context, c *http.Client, u string, form url.Values) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if res, err := c.Do(req); err == nil {
		res.Body.Close()
	}
}
