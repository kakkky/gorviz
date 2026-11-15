package childprocess

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/kakkky/gorviz/domain"
)

// schedtrace の周期 (ms)。scheddetail は全 G を 1 行ずつ出すので、短くすると G の多いプロセスで出力が膨らむ
const schedTracePeriodMs = "1000"

// stdout / stderr は io.Pipe で受ける。exec の StdoutPipe は読み切る前に Wait を呼べないが、
// 読み取りは別の goroutine で続くので、Wait が出力のコピーを待ってから pipe を閉じる形にする
type ChildProcess struct {
	cmd     *exec.Cmd
	stdout  *io.PipeReader
	stderr  *io.PipeReader
	stdoutW *io.PipeWriter
	stderrW *io.PipeWriter
}

// Start は argv を GODEBUG=schedtrace,scheddetail つきで起動する。対象がもともと持っている GODEBUG は残す。
func Start(ctx context.Context, argv []string) (*ChildProcess, error) {
	if len(argv) == 0 {
		return nil, domain.ErrInvalidArgument.With("no command given after --")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), "GODEBUG="+withSchedTrace(os.Getenv("GODEBUG")))
	stdout, stdoutW := io.Pipe()
	stderr, stderrW := io.Pipe()
	cmd.Stdout, cmd.Stderr = stdoutW, stderrW
	if err := cmd.Start(); err != nil {
		return nil, domain.ErrInvalidArgument.Wrap(err, "start "+argv[0])
	}
	return &ChildProcess{cmd: cmd, stdout: stdout, stderr: stderr, stdoutW: stdoutW, stderrW: stderrW}, nil
}

func (c *ChildProcess) Stdout() io.Reader {
	return c.stdout
}

func (c *ChildProcess) Stderr() io.Reader {
	return c.stderr
}

func (c *ChildProcess) Wait() error {
	err := c.cmd.Wait()
	c.stdoutW.Close()
	c.stderrW.Close()
	return err
}

func withSchedTrace(godebug string) string {
	settings := []string{"schedtrace=" + schedTracePeriodMs, "scheddetail=1"}
	if godebug != "" {
		settings = append([]string{godebug}, settings...)
	}
	return strings.Join(settings, ",")
}
