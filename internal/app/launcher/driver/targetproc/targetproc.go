// Package targetproc は、launcher が観測する対象のアプリを子プロセスとして起動する
package targetproc

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// Run は argv を起動し、標準出力と標準エラー出力を out にまとめて流す (2>&1 と同じ)。
// 対象が終わるまで返らない。ctx が終わったら SIGINT を送り、終わらなければ 5 秒後に止める
func Run(ctx context.Context, argv []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, out, out
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("target %s: %w", argv[0], err)
	}
	return nil
}
