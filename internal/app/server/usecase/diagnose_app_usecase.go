package usecase

import (
	"context"
	"fmt"
	"maps"
	"math/rand/v2"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const markWait = 30 * time.Second

// DiagnoseAppUsecase はアプリを動かして Issue を出す。シナリオがあれば smoke・ramp-up・cancellation・cooldown の順に負荷をかけ、
// phase ごとに Issue を判定する。なければ負荷をかけず、アプリが終わるまで (終わらないアプリは決めた時間だけ) 見る
type DiagnoseAppUsecase struct {
	sessions  domain.SessionStore
	index     domain.TraceIndexStore
	issues    domain.IssueBookStore
	agent     domain.AgentInstructor
	requester domain.AppRequester
	toolchain domain.Toolchain
	clock     domain.Clock
	detect    *DetectAppIssuesUsecase
}

// DiagnoseAppUsecaseInput の Progress は phase (ramp-up は level) が終わるたびに呼ぶ
type DiagnoseAppUsecaseInput struct {
	Scenario []byte
	Progress func(domain.DiagnosisProgress)
}

type DiagnoseAppUsecaseOutput struct {
	Summary domain.DiagnosisSummary
}

func (us *UseCase) NewDiagnoseAppUsecase() *DiagnoseAppUsecase {
	return &DiagnoseAppUsecase{sessions: us.sessions, index: us.index, issues: us.issues, agent: us.agent, requester: us.requester, toolchain: us.toolchain, clock: us.clock, detect: us.NewDetectAppIssuesUsecase()}
}

func (u *DiagnoseAppUsecase) Exec(ctx context.Context, in DiagnoseAppUsecaseInput) (DiagnoseAppUsecaseOutput, error) {
	progress := in.Progress
	if progress == nil {
		progress = func(domain.DiagnosisProgress) {}
	}
	var sc domain.Scenario
	if len(in.Scenario) > 0 {
		var err error
		if sc, err = domain.ParseScenario(in.Scenario); err != nil {
			return DiagnoseAppUsecaseOutput{}, err
		}
	}
	s := u.sessions.Session()
	switch s.App.State {
	case domain.AppBuilding, domain.AppStarting:
		return DiagnoseAppUsecaseOutput{}, domain.ErrInvalidArgument.With("the app is still starting; run gorviz status and try again")
	case domain.AppBuildFailed:
		return DiagnoseAppUsecaseOutput{}, domain.ErrInvalidArgument.With("the app failed to build; fix it, then gorviz stop and gorviz run again")
	case domain.AppExited:
		if len(in.Scenario) > 0 {
			return DiagnoseAppUsecaseOutput{}, domain.ErrInvalidArgument.With("the app has already exited, so no load can be sent; gorviz stop and gorviz run again")
		}
		return DiagnoseAppUsecaseOutput{Summary: u.summarizeSession()}, nil
	}
	busy := false
	u.sessions.UpdateSession(func(s *domain.Session) {
		busy = s.Diagnosing
		s.Diagnosing = true
	})
	if busy {
		return DiagnoseAppUsecaseOutput{}, domain.ErrInvalidArgument.With("another gorviz diagnose is running; wait for it to finish")
	}
	defer u.sessions.UpdateSession(func(s *domain.Session) { s.Diagnosing = false })
	if len(in.Scenario) == 0 {
		return u.watch(ctx)
	}
	return u.load(ctx, sc, s.App.Procs, progress)
}

func (u *DiagnoseAppUsecase) summarizeSession() domain.DiagnosisSummary {
	u.waitDetected(context.Background(), -1)
	sum := domain.DiagnosisSummary{}
	u.index.ViewTraceIndex(func(ix *domain.TraceIndex) { sum.To = ix.Last() })
	u.issues.ViewIssueBook(func(b *domain.IssueBook) { sum.Issues = domain.DiagnosedIssues(b, sum.From, sum.To) })
	s := u.sessions.Session()
	sum.AppExited, sum.AppExitCode = s.App.Exited, s.App.ExitCode
	sum.ExitCode = domain.DiagnosisExitCode(sum)
	return sum
}

func (u *DiagnoseAppUsecase) mark(ctx context.Context, message string) (time.Duration, error) {
	n := 0
	u.index.ViewTraceIndex(func(ix *domain.TraceIndex) { _, n = domain.LastMarkAt(ix.Marks(), message) })
	if err := u.agent.WriteTraceEvent(ctx, message); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, markWait)
	defer cancel()
	for {
		var at time.Duration
		m := 0
		u.index.ViewTraceIndex(func(ix *domain.TraceIndex) { at, m = domain.LastMarkAt(ix.Marks(), message) })
		if m > n {
			return at, nil
		}
		if err := u.clock.Sleep(ctx, 20*time.Millisecond); err != nil {
			return 0, domain.ErrInternal.Wrap(err, "wait for the event "+message+" to reach the index")
		}
	}
}

// waitDetected は until まで判定が済み、預けた goroutineleak プロファイルがなくなるまで判定を回す。until が負なら届いた分すべて
func (u *DiagnoseAppUsecase) waitDetected(ctx context.Context, until time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, markWait)
	defer cancel()
	for {
		out, _ := u.detect.Exec(ctx, DetectAppIssuesUsecaseInput{})
		if out.PendingLeaks == 0 && (until < 0 || out.DetectedUntil >= until) {
			return
		}
		if u.clock.Sleep(ctx, 100*time.Millisecond) != nil {
			return
		}
	}
}

func (u *DiagnoseAppUsecase) watch(ctx context.Context) (DiagnoseAppUsecaseOutput, error) {
	from, err := u.mark(ctx, domain.MarkDiagnoseBegin)
	if err != nil {
		return DiagnoseAppUsecaseOutput{}, err
	}
	wctx, cancel := context.WithTimeout(ctx, domain.NoScenarioWatch)
	_ = u.agent.WaitDisconnected(wctx)
	cancel()
	sum := domain.DiagnosisSummary{From: from}
	if u.agent.Connected() {
		to, err := u.mark(ctx, domain.MarkDiagnoseEnd)
		if err != nil {
			return DiagnoseAppUsecaseOutput{}, err
		}
		sum.To = to
		u.waitDetected(ctx, to)
	} else {
		u.waitAppExited(ctx)
		u.waitDetected(ctx, -1)
		u.index.ViewTraceIndex(func(ix *domain.TraceIndex) { sum.To = ix.Last() })
	}
	u.issues.ViewIssueBook(func(b *domain.IssueBook) { sum.Issues = domain.DiagnosedIssues(b, sum.From, sum.To) })
	s := u.sessions.Session()
	sum.AppExited, sum.AppExitCode = s.App.Exited, s.App.ExitCode
	sum.ExitCode = domain.DiagnosisExitCode(sum)
	u.sessions.UpdateSession(func(s *domain.Session) {
		s.Diagnoses = append(s.Diagnoses, domain.DiagnosisRange{From: sum.From, To: sum.To})
	})
	return DiagnoseAppUsecaseOutput{Summary: sum}, nil
}

func (u *DiagnoseAppUsecase) waitAppExited(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, markWait)
	defer cancel()
	for !u.sessions.Session().App.Exited {
		if u.clock.Sleep(ctx, 50*time.Millisecond) != nil {
			return
		}
	}
}

type loadRun struct {
	sc     domain.Scenario
	vars   domain.Vars
	seq    *atomic.Int64
	client domain.LoadClient
}

func (u *DiagnoseAppUsecase) send(ctx context.Context, r loadRun, st domain.Step, vars domain.Vars, cancelAfter time.Duration) (domain.Response, bool) {
	vars["seq"] = strconv.FormatInt(r.seq.Add(1), 10)
	req := r.sc.Request(st, vars)
	req.Cancel = cancelAfter
	resp := r.client.Send(ctx, req)
	passed := st.Passed(resp)
	if passed {
		st.CaptureInto(resp, vars)
	}
	return resp, passed
}

func (u *DiagnoseAppUsecase) load(ctx context.Context, sc domain.Scenario, procs int, progress func(domain.DiagnosisProgress)) (DiagnoseAppUsecaseOutput, error) {
	run := loadRun{sc: sc, vars: domain.Vars{"vu": "0"}, seq: &atomic.Int64{}, client: u.requester.NewLoadClient()}
	if err := u.waitReady(ctx, run); err != nil {
		return DiagnoseAppUsecaseOutput{}, err
	}
	if err := u.agent.ResetCoverage(ctx); err != nil {
		return DiagnoseAppUsecaseOutput{}, err
	}
	from, err := u.mark(ctx, domain.MarkDiagnoseBegin)
	if err != nil {
		return DiagnoseAppUsecaseOutput{}, err
	}
	sum := domain.DiagnosisSummary{From: from, Loaded: true}
	sum.Stopped = u.setup(ctx, run)
	if sum.Stopped == "" {
		sum.Stopped = u.phases(ctx, run, procs, &sum, progress)
	}
	files, covErr := u.agent.ReadCoverage(ctx)
	to, err := u.mark(ctx, domain.MarkDiagnoseEnd)
	if err != nil {
		return DiagnoseAppUsecaseOutput{}, err
	}
	sum.To = to
	u.waitDetected(ctx, to)
	u.judgePhases(from, to)
	if covErr == nil {
		sum.Coverage = u.coverage(ctx, files)
	}
	u.issues.ViewIssueBook(func(b *domain.IssueBook) { sum.Issues = domain.DiagnosedIssues(b, from, to) })
	sum.ExitCode = domain.DiagnosisExitCode(sum)
	u.sessions.UpdateSession(func(s *domain.Session) { s.Diagnoses = append(s.Diagnoses, domain.DiagnosisRange{From: from, To: to}) })
	return DiagnoseAppUsecaseOutput{Summary: sum}, nil
}

func (u *DiagnoseAppUsecase) waitReady(ctx context.Context, run loadRun) error {
	ctx, cancel := context.WithTimeout(ctx, domain.ReadyTimeout)
	defer cancel()
	var last domain.Response
	for {
		resp, passed := u.send(ctx, run, run.sc.Ready, maps.Clone(run.vars), 0)
		if passed {
			return nil
		}
		last = resp
		if u.clock.Sleep(ctx, 200*time.Millisecond) != nil {
			why := last.Err
			if why == "" {
				why = "status " + strconv.Itoa(last.Status)
			}
			return domain.ErrInvalidArgument.With(fmt.Sprintf("the app did not answer the ready request %s %s within %s (%s); check target and ready in the scenario", run.sc.Ready.Method, run.sc.Ready.Path, domain.ReadyTimeout, why))
		}
	}
}

func (u *DiagnoseAppUsecase) setup(ctx context.Context, run loadRun) string {
	for i, st := range run.sc.Setup {
		resp, passed := u.send(ctx, run, st, run.vars, 0)
		if !passed {
			return fmt.Sprintf("setup[%d] %s %s returned %s; fix the scenario", i, st.Method, st.Path, describe(resp))
		}
	}
	return ""
}

func describe(r domain.Response) string {
	if r.Err != "" {
		return "an error (" + r.Err + ")"
	}
	return "status " + strconv.Itoa(r.Status)
}

func (u *DiagnoseAppUsecase) phases(ctx context.Context, run loadRun, procs int, sum *domain.DiagnosisSummary, progress func(domain.DiagnosisProgress)) string {
	if _, err := u.mark(ctx, domain.PhaseMark(domain.PhaseSmoke)); err != nil {
		return err.Error()
	}
	for _, f := range run.sc.Flows {
		vars := maps.Clone(run.vars)
		res := domain.SmokeResult{Flow: f.Name, Passed: true}
		for _, st := range f.Steps {
			resp, passed := u.send(ctx, run, st, vars, 0)
			res.Steps = append(res.Steps, domain.StepResult{Method: st.Method, Path: st.Path, Status: resp.Status, Err: resp.Err, Passed: passed})
			if !passed {
				res.Passed = false
				break
			}
		}
		sum.Smoke = append(sum.Smoke, res)
	}
	progress(domain.DiagnosisProgress{Phase: domain.PhaseSmoke, Smoke: sum.Smoke})
	if passed, total := domain.SmokePassed(sum.Smoke); passed < total {
		return fmt.Sprintf("%d of %d flows did not pass smoke; fix the scenario with the statuses and the coverage", total-passed, total)
	}
	var ramp domain.LoadStats
	ramp.Statuses = map[string]int{}
	for i, c := range domain.RampUpLevels(procs) {
		phase := domain.RampUpPhase(i + 1)
		if _, err := u.mark(ctx, domain.PhaseMark(phase)); err != nil {
			return err.Error()
		}
		st := u.drive(ctx, run, phase, c, domain.RampUpLevelDuration, 0, 0)
		ramp.Merge(st)
		sum.Levels = append(sum.Levels, st)
		progress(domain.DiagnosisProgress{Phase: phase, Load: &st})
		if ctx.Err() != nil {
			return "the diagnosis was canceled"
		}
	}
	if _, err := u.mark(ctx, domain.PhaseMark(domain.PhaseCancellation)); err != nil {
		return err.Error()
	}
	st := u.drive(ctx, run, domain.PhaseCancellation, domain.CancellationConcurrency(procs), domain.CancellationDuration, domain.CancelShare, ramp.Latency.Quantile(0.5))
	sum.Cancellation = &st
	progress(domain.DiagnosisProgress{Phase: domain.PhaseCancellation, Load: &st})
	if _, err := u.mark(ctx, domain.PhaseMark(domain.PhaseCooldown)); err != nil {
		return err.Error()
	}
	if err := u.clock.Sleep(ctx, domain.CooldownWait); err != nil {
		return "the diagnosis was canceled"
	}
	if err := u.agent.RequestGoroutineLeaks(ctx); err != nil {
		return err.Error()
	}
	progress(domain.DiagnosisProgress{Phase: domain.PhaseCooldown, Note: "took the goroutineleak profile and the coverage after the load"})
	return ""
}

// drive は同時実行数の VU で、期間のあいだ flow を重みに沿って送り続ける。期間の終わりで切ったリクエストは数えない
func (u *DiagnoseAppUsecase) drive(ctx context.Context, run loadRun, phase string, concurrency int, d time.Duration, cancelShare float64, median time.Duration) domain.LoadStats {
	results := make([]domain.LoadStats, concurrency)
	start := u.clock.Now()
	pctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	_ = scope.Run(pctx, func(s *scope.Scope) error {
		for i := range concurrency {
			s.Go(func(ctx context.Context) error {
				results[i] = u.vu(ctx, run, phase, concurrency, i+1, cancelShare, median)
				return nil
			})
		}
		return nil
	})
	out := domain.NewLoadStats(phase, concurrency)
	for _, r := range results {
		out.Merge(r)
	}
	out.Duration = u.clock.Now().Sub(start)
	return out
}

func (u *DiagnoseAppUsecase) vu(ctx context.Context, run loadRun, phase string, concurrency, n int, cancelShare float64, median time.Duration) domain.LoadStats {
	st := domain.NewLoadStats(phase, concurrency)
	vr := run
	vr.client = u.requester.NewLoadClient()
	rng := rand.New(rand.NewPCG(uint64(n), uint64(u.clock.Now().UnixNano())))
	for ctx.Err() == nil {
		vars := maps.Clone(run.vars)
		vars["vu"] = strconv.Itoa(n)
		for _, step := range run.sc.PickFlow(rng.Float64()).Steps {
			var cancelAfter time.Duration
			if cancelShare > 0 && rng.Float64() < cancelShare {
				cancelAfter = domain.CancelAfter(median, rng.Float64())
			}
			resp, passed := u.send(ctx, vr, step, vars, cancelAfter)
			if ctx.Err() != nil {
				return st
			}
			st.Add(resp, passed)
			if !passed {
				break
			}
		}
	}
	return st
}

// judgePhases は ramp-up の level ごと・cancellation の期間で、GOROUTINE_LEAK 以外を判定し直す
func (u *DiagnoseAppUsecase) judgePhases(from, to time.Duration) {
	type phased struct {
		d     domain.Detection
		phase string
	}
	var found []phased
	u.index.ViewTraceIndex(func(ix *domain.TraceIndex) {
		for _, p := range domain.PhasesOf(ix.Marks(), ix.Last()) {
			if p.From < from || p.To > to || p.Name == domain.PhaseSmoke || p.Name == domain.PhaseCooldown {
				continue
			}
			for _, d := range domain.DetectRuntimeIssues(domain.NewWindowStats(ix, p.From, p.To)) {
				found = append(found, phased{d: d, phase: p.Name})
			}
		}
	})
	u.issues.UpdateIssueBook(func(b *domain.IssueBook) {
		for _, f := range found {
			b.RecordPhase(f.d, f.phase)
		}
	})
}

func (u *DiagnoseAppUsecase) coverage(ctx context.Context, files []domain.CoverageFile) *domain.CoverageReport {
	text, err := u.toolchain.CoverageProfile(ctx, files)
	if err != nil {
		return nil
	}
	build := u.sessions.Session().Build
	blocks, err := domain.ParseCoverageProfile(text)
	if err != nil {
		return nil
	}
	r := domain.BuildCoverageReport(blocks, u.toolchain.ReadAppSources(build, domain.CoverageFilesOf(blocks)))
	return &r
}
