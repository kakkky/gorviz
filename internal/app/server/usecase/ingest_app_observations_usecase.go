package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

type ObservationKind int

const (
	ObservationHello ObservationKind = iota + 1
	ObservationTrace
	ObservationGoroutineLeakProfile
	ObservationCoverage
	ObservationEnd
)

// IngestAppObservationsUsecase は agent から届いた観測データを受け取る。trace は index にする読み手へ流し、
// goroutineleak プロファイルは取った時刻の trace が index に届くまで預ける
type IngestAppObservationsUsecase struct {
	sessions domain.SessionStore
	issues   domain.IssueBookStore
	stream   domain.TraceStream
}

// IngestAppObservationsUsecaseInput の Broken は ObservationEnd のとき、終わりの合図を受けずに接続が切れたこと
type IngestAppObservationsUsecaseInput struct {
	Kind     ObservationKind
	Hello    domain.AppRun
	Trace    []byte
	LeakSeq  uint64
	LeakText []byte
	Coverage []domain.CoverageFile
	Broken   bool
}

type IngestAppObservationsUsecaseOutput struct{}

func (us *UseCase) NewIngestAppObservationsUsecase() *IngestAppObservationsUsecase {
	return &IngestAppObservationsUsecase{sessions: us.sessions, issues: us.issues, stream: us.stream}
}

func (u *IngestAppObservationsUsecase) Exec(_ context.Context, in IngestAppObservationsUsecaseInput) (IngestAppObservationsUsecaseOutput, error) {
	switch in.Kind {
	case ObservationHello:
		u.sessions.UpdateSession(func(s *domain.Session) {
			s.App.GoVersion, s.App.Procs, s.App.PID = in.Hello.GoVersion, in.Hello.Procs, in.Hello.PID
			s.Observed.Procs = in.Hello.Procs
		})
	case ObservationTrace:
		return IngestAppObservationsUsecaseOutput{}, u.stream.AppendTrace(in.Trace)
	case ObservationGoroutineLeakProfile:
		stacks, err := domain.ParseGoroutineLeakProfile(in.LeakText)
		if err != nil {
			return IngestAppObservationsUsecaseOutput{}, err
		}
		u.issues.UpdateIssueBook(func(b *domain.IssueBook) {
			b.Pending = append(b.Pending, domain.PendingLeakCheck{Seq: in.LeakSeq, Stacks: stacks})
		})
	case ObservationCoverage:
		u.sessions.UpdateSession(func(s *domain.Session) { s.LastCoverage = in.Coverage })
	case ObservationEnd:
		u.stream.CloseTrace(in.Broken)
	}
	return IngestAppObservationsUsecaseOutput{}, nil
}
