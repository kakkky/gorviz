package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const shutdownWait = time.Minute

// WaitAppShutdownUsecase はアプリが終わるのを待ち、shutdown の結果 (終了コード・時間内に終わったか・終わった時点で残っていた goroutine) を返す
type WaitAppShutdownUsecase struct {
	sessions domain.SessionStore
	issues   domain.IssueBookStore
	clock    domain.Clock
	detect   *DetectAppIssuesUsecase
}

type WaitAppShutdownUsecaseInput struct{}

type WaitAppShutdownUsecaseOutput struct {
	App       domain.AppRun
	Remaining []domain.LeakStack
	Issues    []domain.DiagnosedIssue
}

func (us *UseCase) NewWaitAppShutdownUsecase() *WaitAppShutdownUsecase {
	return &WaitAppShutdownUsecase{sessions: us.sessions, issues: us.issues, clock: us.clock, detect: us.NewDetectAppIssuesUsecase()}
}

func (u *WaitAppShutdownUsecase) Exec(ctx context.Context, _ WaitAppShutdownUsecaseInput) (WaitAppShutdownUsecaseOutput, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownWait)
	defer cancel()
	for {
		s := u.sessions.Session()
		if s.App.Exited || s.App.State == domain.AppBuildFailed || s.App.State == "" {
			break
		}
		if err := u.clock.Sleep(ctx, 50*time.Millisecond); err != nil {
			return WaitAppShutdownUsecaseOutput{}, domain.ErrInternal.Wrap(err, "wait for the app to shut down")
		}
	}
	_, _ = u.detect.Exec(ctx, DetectAppIssuesUsecaseInput{})
	out := WaitAppShutdownUsecaseOutput{App: u.sessions.Session().App}
	u.issues.ViewIssueBook(func(b *domain.IssueBook) {
		for _, is := range b.Issues() {
			for _, o := range is.Occurrences {
				if o.Phase != domain.PhaseShutdown {
					continue
				}
				out.Issues = append(out.Issues, domain.DiagnosedIssue{ID: is.ID, Kind: is.Kind, Target: is.TargetLabel(), Certainty: is.Certainty(), FirstPhase: o.Phase, Occurrences: 1})
				if is.Kind == domain.IssueGoroutineLeak {
					out.Remaining = append(out.Remaining, o.Leaks...)
				}
				break
			}
		}
	})
	return out, nil
}
