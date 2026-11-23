package logsource

import (
	"bufio"
	"context"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/logs"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// StdinReader はパイプで渡された行を読む。echo なら、取り込みながら端末にもそのまま出す
type StdinReader struct {
	receiver *logs.Receiver
	enabled  bool
	echo     io.Writer
	maxBytes int
}

func NewStdinReader(cfg *config.Config, receiver *logs.Receiver) (*StdinReader, error) {
	s := &StdinReader{receiver: receiver, enabled: cfg.Stdin, maxBytes: cfg.LogMaxLineBytes}
	if cfg.StdinEcho {
		s.echo = os.Stdout
	}
	return s, nil
}

// Run は標準入力が閉じたら (パイプの書き手が終わったら) 抜ける。ほかの取り込みは止めない
func (s *StdinReader) Run(ctx context.Context) error {
	if !s.enabled {
		<-ctx.Done()
		return nil
	}
	// 読みかけの Read を ctx の終わりで止めるために、標準入力をノンブロッキングにして期限を付けられるようにする
	_ = syscall.SetNonblock(syscall.Stdin, true)
	in := os.NewFile(uintptr(syscall.Stdin), "stdin")
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(func(ctx context.Context) error {
			<-ctx.Done()
			_ = in.SetReadDeadline(time.Now())
			return nil
		})
		sc.Go(func(ctx context.Context) error {
			s.read(ctx, in)
			return nil
		})
		return nil
	}, scope.WithCancelOnSuccess())
}

func (s *StdinReader) read(ctx context.Context, in io.Reader) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), s.maxBytes)
	for sc.Scan() {
		if s.echo != nil {
			_, _ = s.echo.Write(sc.Bytes())
			_, _ = s.echo.Write([]byte{'\n'})
		}
		s.receiver.Receive(ctx, observation.LogSourceStdin, "", string(trimEOL(sc.Bytes())))
	}
}
