package usecase

import (
	"context"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const (
	agentFinishWait = 5 * time.Second
	indexCatchUp    = 10 * time.Second
)

// RunAppUsecase はアプリに agent を差し込んでビルドし、子プロセスとして起動して、終わるまで見届ける。
// server が止まるときは、止める合図を送って graceful shutdown を見届けてから終わる
type RunAppUsecase struct {
	sessions domain.SessionStore
	index    domain.TraceIndexStore
	stream   domain.TraceStream
	agent    domain.AgentInstructor
	builder  domain.AppBuilder
	starter  domain.AppStarter
	sources  domain.AgentSources
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
	return &RunAppUsecase{sessions: us.sessions, index: us.index, stream: us.stream, agent: us.agent, builder: us.builder, starter: us.starter, sources: us.sources, clock: us.clock}
}

func (u *RunAppUsecase) Exec(ctx context.Context, in RunAppUsecaseInput) (RunAppUsecaseOutput, error) {
	u.sessions.UpdateSession(func(s *domain.Session) {
		s.App = domain.AppRun{Package: in.Package, Args: in.Args, State: domain.AppBuilding}
	})
	build, err := u.build(ctx, in)
	if err != nil {
		u.stream.CloseTrace(false)
		u.sessions.UpdateSession(func(s *domain.Session) {
			s.App.State, s.App.BuildError = domain.AppBuildFailed, err.Error()
		})
		return RunAppUsecaseOutput{}, nil
	}
	u.sessions.UpdateSession(func(s *domain.Session) { s.Build, s.App.State = build, domain.AppStarting })
	proc, err := u.starter.StartApp(ctx, build, in.Args, in.Env)
	if err != nil {
		u.stream.CloseTrace(false)
		u.sessions.UpdateSession(func(s *domain.Session) {
			s.App.State, s.App.StartError = domain.AppExited, err.Error()
		})
		return RunAppUsecaseOutput{}, nil
	}
	u.sessions.UpdateSession(func(s *domain.Session) { s.App.State = domain.AppRunning })

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
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), agentFinishWait)
	defer cancel()
	_ = u.agent.WaitDisconnected(wctx)
	u.stream.CloseTrace(true)
	u.waitIndexEnded(context.WithoutCancel(ctx))
	var exitedAt time.Duration
	u.index.ViewTraceIndex(func(ix *domain.TraceIndex) { exitedAt = ix.Last() })
	u.sessions.UpdateSession(func(s *domain.Session) {
		s.App.State, s.App.Exited, s.App.ExitCode, s.App.ExitedAt, s.App.ShutdownTimedOut = domain.AppExited, true, code, exitedAt, timedOut
		s.App.TraceTruncated = u.stream.TraceBroken()
	})
	return RunAppUsecaseOutput{}, nil
}

func (u *RunAppUsecase) build(ctx context.Context, in RunAppUsecaseInput) (domain.AppBuild, error) {
	bin := domain.BuildAppInput{Dir: in.Dir, Package: in.Package, Env: in.Env}
	main, build, err := u.builder.LoadMainPackage(ctx, bin)
	if err != nil {
		return domain.AppBuild{}, err
	}
	agent, socket, err := u.sources.AgentFiles()
	if err != nil {
		return domain.AppBuild{}, err
	}
	overlay, err := domain.AgentOverlay(main, agent, socket)
	if err != nil {
		return domain.AppBuild{}, err
	}
	if err := u.builder.BuildApp(ctx, bin, build, overlay); err != nil {
		return domain.AppBuild{}, err
	}
	return build, nil
}

// shutdown は止める合図を送ったあとも trace を流させ、時間内に終わらなければ最後の観測データを送らせてから強制終了する
func (u *RunAppUsecase) shutdown(ctx context.Context, proc domain.AppProcess, exited <-chan int, timeout time.Duration) (int, bool) {
	ictx, cancel := context.WithTimeout(ctx, time.Second)
	_ = u.agent.WriteTraceEvent(ictx, domain.MarkShutdown)
	cancel()
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
	_ = u.agent.Finish(ctx)
	wctx, cancel := context.WithTimeout(ctx, agentFinishWait)
	_ = u.agent.WaitDisconnected(wctx)
	cancel()
	_ = proc.Kill()
	return <-exited, true
}

func (u *RunAppUsecase) waitIndexEnded(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, indexCatchUp)
	defer cancel()
	for {
		ended := false
		u.index.ViewTraceIndex(func(ix *domain.TraceIndex) { ended, _ = ix.Ended() })
		if ended || u.clock.Sleep(ctx, 50*time.Millisecond) != nil {
			return
		}
	}
}
