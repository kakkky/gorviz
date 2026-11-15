// Package launcher はローカルで server と agent を 1 つのプロセスでまとめて動かす (gorviz-launcher)。
// プロセスは 1 つでも server と agent は別々のアプリのままで、間は gorviz-server と gorviz-agent と同じく TCP でつなぐ
package launcher

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/agent"
	"github.com/kakkky/gorviz/internal/app/launcher/config"
	"github.com/kakkky/gorviz/internal/app/server"
)

// Run は server と agent を回す。どちらかが終わったら、もう片方も止める
func Run(ctx context.Context, cfg *config.Config, stdin io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(func(ctx context.Context) error {
			defer cancel()
			return server.Run(ctx, cfg.Server)
		})
		sc.Go(func(ctx context.Context) error {
			defer cancel()
			// server が待ち受けを始めてからつなぐ。つながらなくても agent がつなぎ直すので、待つのは見た目のため
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(300 * time.Millisecond):
			}
			return agent.RunTargets(ctx, cfg.Agent, stdin)
		})
		fmt.Fprintf(os.Stderr, "gorviz: open http://%s\n", cfg.Server.HTTPAddr)
		return nil
	})
}
