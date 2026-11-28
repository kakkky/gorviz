package usecase

import (
	"context"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const indexCatchUp = 10 * time.Second

// RunAppUsecase はアプリに pprof を開く処理を差し込んでビルドし、子プロセスとして起動して、終わるまで見届ける。
// server が止まるときは、止める合図を送って graceful shutdown を見届けてから終わる
type RunAppUsecase struct {
	sessions domain.SessionStore
	issues   domain.IssueBookStore
	builder  domain.AppBuilder
	starter  domain.AppStarter
	profiler domain.AppProfiler
	clock    domain.Clock
}

type RunAppUsecaseInput struct {
	Dir             string
	Package         string
	Args            []string
	Env             []string
	ShutdownTimeout time.Duration
}

type RunAppUsecaseOutput struct{}

func (us *UseCase) NewRunAppUsecase() *RunAppUsecase {
	return &RunAppUsecase{sessions: us.sessions, issues: us.issues, builder: us.builder, starter: us.starter, profiler: us.profiler, clock: us.clock}
}

func (u *RunAppUsecase) Exec(ctx context.Context, in RunAppUsecaseInput) (RunAppUsecaseOutput, error) {
	u.sessions.UpdateSession(func(s *domain.Session) {
		s.App = domain.AppRun{Package: in.Package, Args: in.Args, State: domain.AppBuilding}
	})
	build, err := u.build(ctx, in)
	if err != nil {
		u.sessions.UpdateSession(func(s *domain.Session) {
			s.App.State, s.App.BuildError = domain.AppBuildFailed, err.Error()
		})
		return RunAppUsecaseOutput{}, nil
	}
	version, _ := u.builder.GoVersion(build)
	u.sessions.UpdateSession(func(s *domain.Session) { s.Build, s.App.State, s.App.GoVersion = build, domain.AppStarting, version })
	proc, err := u.starter.StartApp(ctx, build, in.Args, in.Env)
	if err != nil {
		u.sessions.UpdateSession(func(s *domain.Session) {
			s.App.State, s.App.StartError, s.App.ProcessExited = domain.AppExited, err.Error(), true
		})
		return RunAppUsecaseOutput{}, nil
	}
	u.sessions.UpdateSession(func(s *domain.Session) { s.App.State, s.App.PID = domain.AppRunning, proc.PID() })

	var code int
	timedOut := false
	err = scope.Run(context.WithoutCancel(ctx), func(sc *scope.Scope) error {
		exited := make(chan int, 1)
		sc.Go(func(context.Context) error {
			exited <- proc.Wait()
			return nil
		})
		select {
		case code = <-exited:
			return nil
		case <-ctx.Done():
		}
		code, timedOut = u.shutdown(context.WithoutCancel(ctx), proc, exited, in.ShutdownTimeout)
		return nil
	})
	if err != nil {
		return RunAppUsecaseOutput{}, domain.ErrInternal.Wrap(err, "watch the app")
	}
	u.sessions.UpdateSession(func(s *domain.Session) { s.App.ProcessExited = true })
	u.waitIndexEnded(context.WithoutCancel(ctx))
	observed := u.sessions.Session().Observed
	u.sessions.UpdateSession(func(s *domain.Session) {
		s.App.State, s.App.Exited, s.App.ExitCode, s.App.ExitedAt, s.App.ShutdownTimedOut = domain.AppExited, true, code, observed.Until, timedOut
		s.App.TraceTruncated = observed.Broken
	})
	return RunAppUsecaseOutput{}, nil
}

func (u *RunAppUsecase) build(ctx context.Context, in RunAppUsecaseInput) (domain.AppBuild, error) {
	bin := domain.BuildAppInput{Dir: in.Dir, Package: in.Package, Env: in.Env}
	main, build, err := u.builder.LoadMainPackage(ctx, bin)
	if err != nil {
		return domain.AppBuild{}, err
	}
	if err := u.builder.BuildApp(ctx, bin, build, domain.PprofOverlay(main)); err != nil {
		return domain.AppBuild{}, err
	}
	return build, nil
}

// shutdown は止める合図を送ったあとも trace を取らせ、時間内に終わらなければ、取りかけの区切りを取り終えるのを待ってから強制終了する。
// 合図の直前に goroutineleak プロファイルを取る。すぐ終わるアプリでも、shutdown の間に 1 回は取れているようにするため
func (u *RunAppUsecase) shutdown(ctx context.Context, proc domain.AppProcess, exited <-chan int, timeout time.Duration) (int, bool) {
	now := u.clock.Now()
	if at, ok := u.sessions.Session().Observed.TraceAt(now); ok {
		u.sessions.UpdateSession(func(s *domain.Session) { s.Observed.AddMarks(domain.TraceMark{At: at, Message: domain.MarkShutdown}) })
		if err := takeGoroutineLeaks(ctx, u.profiler, u.sessions, u.issues, now); err == nil {
			u.sessions.UpdateSession(func(s *domain.Session) { s.Collected.Leaks = now })
		}
	}
	if err := proc.Terminate(); err != nil {
		_ = proc.Kill()
		return <-exited, false
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case code := <-exited:
		return code, false
	case <-t.C:
	}
	_ = u.clock.Sleep(ctx, 2*domain.TraceChunkLength)
	_ = proc.Kill()
	return <-exited, true
}

func (u *RunAppUsecase) waitIndexEnded(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, indexCatchUp)
	defer cancel()
	for {
		if u.sessions.Session().Observed.Ended || u.clock.Sleep(ctx, 50*time.Millisecond) != nil {
			return
		}
	}
}
