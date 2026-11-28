package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// takeGoroutineLeaks はアプリの pprof から goroutineleak プロファイルを取る。取った時刻 (now) を trace の上に置き、その時刻の trace が index に届くまで預ける
func takeGoroutineLeaks(ctx context.Context, profiler domain.AppProfiler, sessions domain.SessionStore, issues domain.IssueBookStore, now time.Time) error {
	at, ok := sessions.Session().Observed.TraceAt(now)
	if !ok {
		return domain.ErrInternal.With("no clock of the trace to put the goroutineleak profile on")
	}
	text, err := profiler.FetchGoroutineLeakProfile(ctx)
	if err != nil {
		return err
	}
	stacks, err := domain.ParseGoroutineLeakProfile(text)
	if err != nil {
		return err
	}
	issues.UpdateIssueBook(func(b *domain.IssueBook) {
		b.Pending = append(b.Pending, domain.PendingLeakCheck{At: at, Stacks: stacks})
	})
	return nil
}
