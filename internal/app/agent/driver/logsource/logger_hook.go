package logsource

import (
	"bytes"
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/logs"
	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/loggerhook"
)

// LoggerHook はロガーアダプタから、同じプロセスの中で渡された行を受ける (ライブラリ型)。
// 渡す関数はアプリのログの呼び出しの中で動くので、詰まっていたら待たずに捨てる
type LoggerHook struct {
	receiver *logs.Receiver
	queue    chan []byte
}

func NewLoggerHook(cfg *config.Config, receiver *logs.Receiver) (*LoggerHook, func(), error) {
	h := &LoggerHook{receiver: receiver, queue: make(chan []byte, cfg.LogQueueLength)}
	loggerhook.SetForward(h.offer)
	return h, func() { loggerhook.SetForward(nil) }, nil
}

func (h *LoggerHook) offer(line []byte) {
	select {
	case h.queue <- line:
	default:
	}
}

func (h *LoggerHook) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case b := <-h.queue:
			for line := range bytes.SplitSeq(b, []byte("\n")) {
				if line = trimEOL(line); len(line) > 0 {
					h.receiver.Receive(ctx, observation.LogSourceLogger, "", string(line))
				}
			}
		}
	}
}
