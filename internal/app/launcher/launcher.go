// Package launcher はローカルで server と agent を 1 つのプロセスでまとめて動かす (gorviz-launcher)。
// プロセスは 1 つでも server と agent は別々のアプリのままで、間は gorviz-server と gorviz-agent と同じく TCP でつなぐ。
// 対象のコマンドを渡されたら、対象も子プロセスとして起動し、その出力をログとして agent に渡す
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
	"github.com/kakkky/gorviz/internal/app/launcher/driver/targetproc"
	"github.com/kakkky/gorviz/internal/app/server"
)

// Run は server と agent (と対象) を回す。どれかが終わったら、残りも止める
func Run(ctx context.Context, cfg *config.Config, stdin io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return scope.Run(ctx, func(sc *scope.Scope) error {
		logs := stdin
		var targetOut *io.PipeReader
		if len(cfg.TargetCommand) > 0 {
			r, w := io.Pipe()
			targetOut = r
			// 対象の出力は端末にも出す。agent が読んだ分だけ流れる
			logs = io.TeeReader(r, os.Stdout)
			sc.Go(func(ctx context.Context) error {
				defer cancel()
				defer w.Close()
				return targetproc.Run(ctx, cfg.TargetCommand, w)
			})
		}
		sc.Go(func(ctx context.Context) error {
			defer cancel()
			return server.Run(ctx, cfg.Server)
		})
		sc.Go(func(ctx context.Context) error {
			defer cancel()
			if targetOut != nil {
				// agent が止まったら対象の出力を読む者がいなくなるので、対象が書き込みで止まらないよう閉じる
				defer targetOut.Close()
			}
			// server が待ち受けを始めてからつなぐ。つながらなくても agent がつなぎ直すので、待つのは見た目のため
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(300 * time.Millisecond):
			}
			return agent.RunTargets(ctx, cfg.Agent, logs)
		})
		fmt.Fprintf(os.Stderr, "gorviz: open http://%s\n", cfg.Server.HTTPAddr)
		return nil
	})
}
