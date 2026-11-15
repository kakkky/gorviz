package loginput

import (
	"bufio"
	"context"
	"io"
	"time"

	"github.com/kakkky/gorviz/internal/usecase"
)

type LogInput struct {
	recordLogLineUsecase *usecase.RecordLogLineUsecase
}

func NewLogInput(uc *usecase.UseCase) *LogInput {
	return &LogInput{recordLogLineUsecase: uc.NewRecordLogLineUsecase()}
}

// Consume は r を 1 行ずつログとして取り込み、echo にもそのまま流す。r を読み切るか ctx が終わるまで返らない
func (l *LogInput) Consume(ctx context.Context, r io.Reader, source string, echo io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := sc.Text()
		if _, err := l.recordLogLineUsecase.Exec(ctx, usecase.RecordLogLineUsecaseInput{Raw: line, Source: source, ReceivedAt: time.Now()}); err != nil {
			return err
		}
		if _, err := io.WriteString(echo, line+"\n"); err != nil {
			return err
		}
	}
	return sc.Err()
}
