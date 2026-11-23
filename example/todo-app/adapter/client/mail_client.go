package client

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

var errMailUnavailable = errors.New("mail service unavailable (503)")

// MailClient は完了メールを外部のメール送信サービスに渡す。
// 不具合 (エラーが増える): 送信サービスは 2 分ごとに 30 秒落ちる。その間の完了はエラーになる
type MailClient struct{}

func NewMailClient() (*MailClient, error) {
	return &MailClient{}, nil
}

func (c *MailClient) SendCompleted(ctx context.Context, t domain.Todo) error {
	time.Sleep(time.Duration(20+rand.IntN(40)) * time.Millisecond)
	if time.Now().Unix()%120 < 30 {
		return domain.ErrInternal.Wrap(errMailUnavailable, "send completion mail")
	}
	return nil
}
