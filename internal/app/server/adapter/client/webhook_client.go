// Package client は通知の送り先への送信を持つ
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const sendTimeout = 10 * time.Second

func postJSON(ctx context.Context, hc *http.Client, url string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return domain.ErrInternal.Wrap(err, "encode a notification")
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return domain.ErrInternal.Wrap(err, "build a notification request")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		return domain.ErrInternal.Wrap(err, "send a notification")
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	if res.StatusCode >= 300 {
		return domain.ErrInternal.Wrap(fmt.Errorf("status %d", res.StatusCode), "send a notification")
	}
	return nil
}

// WebhookClient は gorviz の形の JSON を POST する
type WebhookClient struct{ http *http.Client }

func NewWebhookClient() (*WebhookClient, error) {
	return &WebhookClient{http: &http.Client{}}, nil
}

type webhookBody struct {
	Event string `json:"event"`
	domain.Notification
}

func (c *WebhookClient) Send(ctx context.Context, dest domain.Destination, n domain.Notification) error {
	return postJSON(ctx, c.http, dest.URL, webhookBody{Event: "issue.occurrence.started", Notification: n})
}

// SlackClient は Slack の Incoming Webhook の形で POST する
type SlackClient struct{ http *http.Client }

func NewSlackClient() (*SlackClient, error) {
	return &SlackClient{http: &http.Client{}}, nil
}

func (c *SlackClient) Send(ctx context.Context, dest domain.Destination, n domain.Notification) error {
	text := fmt.Sprintf("*%s* `%s` (%s) が %s で発生しました\n%s\n次に見ること: %s\n<%s|gorviz で開く>",
		n.Kind, n.Subject, n.Certainty, n.Instance, n.Reason, n.NextAction, n.URL)
	return postJSON(ctx, c.http, dest.URL, map[string]any{"text": text})
}
