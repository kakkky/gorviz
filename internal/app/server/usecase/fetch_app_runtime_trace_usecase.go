package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const fetchRetryWait = 20 * time.Millisecond

// FetchAppRuntimeTraceUsecase は、アプリが終わるまで、アプリの pprof から trace の区切りとスレッドの数を取り続け、取った順に読み手へ流す。
// 取れなければ (アプリが pprof を開く前・アプリが終わるところ) 少し待って取り直し、アプリが終わっていたら trace を閉じて返す
type FetchAppRuntimeTraceUsecase struct {
	sessions domain.SessionStore
	stream   domain.TraceStream
	profiler domain.AppProfiler
	clock    domain.Clock
}

type FetchAppRuntimeTraceUsecaseInput struct{}

type FetchAppRuntimeTraceUsecaseOutput struct {
	Chunks int
}

func (us *UseCase) NewFetchAppRuntimeTraceUsecase() *FetchAppRuntimeTraceUsecase {
	return &FetchAppRuntimeTraceUsecase{sessions: us.sessions, stream: us.stream, profiler: us.profiler, clock: us.clock}
}

func (u *FetchAppRuntimeTraceUsecase) Exec(ctx context.Context, _ FetchAppRuntimeTraceUsecaseInput) (FetchAppRuntimeTraceUsecaseOutput, error) {
	defer u.stream.CloseTrace()
	var out FetchAppRuntimeTraceUsecaseOutput
	for {
		trace, err := u.profiler.FetchTrace(ctx, domain.TraceChunkLength)
		if err != nil {
			if appGone(u.sessions.Session().App) {
				return out, nil
			}
			if err := u.clock.Sleep(ctx, fetchRetryWait); err != nil {
				return out, domain.ErrInternal.Wrap(err, "wait to fetch the execution trace again")
			}
			continue
		}
		threads := 0
		if p, err := u.profiler.FetchThreadCreateProfile(ctx); err == nil {
			threads = domain.ParseThreadCreateProfile(p)
		}
		if err := u.stream.AppendTrace(domain.TraceChunk{Trace: trace, Threads: threads}); err != nil {
			return out, err
		}
		out.Chunks++
	}
}

// appGone は、アプリがもう trace を返さないか。ビルドや起動に失敗したときも含む
func appGone(a domain.AppRun) bool {
	return a.ProcessExited || a.State == domain.AppBuildFailed
}
