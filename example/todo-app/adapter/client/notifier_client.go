package client

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

// NotifierClient はリマインダーを外部の通知サービスに登録する。通知サービスは遅いことがある (0〜2 秒)
type NotifierClient struct {
	timeout time.Duration
}

func NewNotifierClient(timeout time.Duration) (*NotifierClient, error) {
	return &NotifierClient{timeout: timeout}, nil
}

// RegisterReminder は timeout で諦める。
// 不具合 (GOROUTINE_LEAK): 送り手の goroutine は結果を buffer のない channel に送るので、タイムアウトで受け手が先に返ると、送り手は二度と送れずに残り続ける。
// リークを起こすための例なので、scope の下ではなく go 文で起動している
func (c *NotifierClient) RegisterReminder(ctx context.Context, t domain.Todo) error {
	result := make(chan error)
	go func() {
		time.Sleep(time.Duration(rand.IntN(2000)) * time.Millisecond)
		result <- nil
	}()
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return domain.ErrInternal.Wrap(ctx.Err(), "register reminder")
	}
}
