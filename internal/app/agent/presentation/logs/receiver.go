package logs

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// Receiver はログの取り込み元から 1 行ずつ受け取る
type Receiver struct {
	uc           *usecase.ForwardLogLineUsecase
	agent        observation.AgentID
	durationKeys []string
}

func NewReceiver(cfg *config.Config, uc *usecase.ForwardLogLineUsecase) *Receiver {
	return &Receiver{uc: uc, agent: cfg.AgentID, durationKeys: cfg.LogDurationKeys}
}

// Receive の path はファイルから読んだときのファイルのパス。どのアプリのログかを決めるのに使う
func (r *Receiver) Receive(ctx context.Context, source observation.LogSource, path, line string) {
	_, err := r.uc.Exec(ctx, usecase.ForwardLogLineUsecaseInput{
		Agent:        r.agent,
		Source:       source,
		Path:         path,
		Raw:          line,
		ReceivedAt:   time.Now(),
		DurationKeys: r.durationKeys,
	})
	if err != nil {
		slog.WarnContext(ctx, "forward log line", slog.String("source", string(source)), slog.Int("bytes", len(line)), slog.Any("err", err))
	}
}
