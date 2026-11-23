package usecase

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const (
	// detectSpanPerExec は 1 回の Exec で 1 つのインスタンスについて判定する観測データの長さ
	detectSpanPerExec = 10 * time.Minute
	// leakGrowthWindow は生成元ごとの待機中の数の推移を見る長さ
	leakGrowthWindow = 10 * time.Minute
	// baselineLag はメトリクスの集計を待つ長さ。これより前の 5 分だけを普段と比べる
	baselineLag           = time.Minute
	baselineWindowsPerRun = 12
)

type DetectIssuesUsecaseInput struct {
	Now time.Time
}

type DetectIssuesUsecaseOutput struct {
	Started  int
	Notified int
}

type DetectIssuesUsecase struct {
	gui           domain.GUI
	notifications map[observation.ServiceName][]domain.Destination
	inventory     domain.InventoryAccessor
	holdings      domain.HoldingAccessor
	lifecycle     domain.LifecycleAccessor
	checkpoints   domain.CheckpointAccessor
	traces        domain.TraceAccessor
	snapshots     domain.SnapshotAccessor
	leakChecks    domain.LeakCheckAccessor
	metrics       domain.MetricAccessor
	issues        domain.IssueAccessor
	preserved     domain.PreservedTraceAccessor
	senders       map[domain.DestinationKind]domain.NotificationClient
}

func (us *UseCase) NewDetectIssuesUsecase() *DetectIssuesUsecase {
	return &DetectIssuesUsecase{
		gui: us.settings.GUI, notifications: us.settings.Notifications, inventory: us.inventory, holdings: us.holdings, lifecycle: us.lifecycle,
		checkpoints: us.checkpoints, traces: us.traces, snapshots: us.snapshots, leakChecks: us.leakChecks, metrics: us.metrics,
		issues: us.issues, preserved: us.preserved,
		senders: map[domain.DestinationKind]domain.NotificationClient{domain.DestinationWebhook: us.webhook, domain.DestinationSlack: us.slack, domain.DestinationMail: us.mail},
	}
}

func (u *DetectIssuesUsecase) Exec(ctx context.Context, in DetectIssuesUsecaseInput) (DetectIssuesUsecaseOutput, error) {
	services, err := u.inventory.ListServices(ctx)
	if err != nil {
		return DetectIssuesUsecaseOutput{}, err
	}
	var out DetectIssuesUsecaseOutput
	for _, s := range services {
		started, notified, err := u.detectService(ctx, s, in.Now)
		if err != nil {
			return DetectIssuesUsecaseOutput{}, err
		}
		out.Started += started
		out.Notified += notified
	}
	return out, nil
}

func (u *DetectIssuesUsecase) detectService(ctx context.Context, s observation.ServiceName, now time.Time) (int, int, error) {
	cps, err := u.checkpoints.ReadCheckpoints(ctx, s)
	if err != nil {
		return 0, 0, err
	}
	instances, err := u.inventory.ListInstances(ctx, s)
	if err != nil {
		return 0, 0, err
	}
	open := map[string][]domain.TrackedOccurrence{}
	for k, cp := range cps[domain.ProcessDetectIssues] {
		open[k] = cp.Open
	}
	open[domain.ServiceKey] = cps.Get(domain.ProcessDetectIssuesBaseline, domain.ServiceKey).Open
	started, notified := 0, 0
	for _, i := range instances {
		cp := cps.Get(domain.ProcessDetectIssues, string(i))
		tracker, next, err := u.detectInstance(ctx, s, i, cp)
		if err != nil {
			return started, notified, err
		}
		if err := u.finish(ctx, s, tracker); err != nil {
			return started, notified, err
		}
		open[string(i)] = tracker.Open()
		notified += u.notify(ctx, s, tracker, open, string(i))
		started += len(tracker.Started())
		next.Open = tracker.Open()
		if err := u.checkpoints.UpdateCheckpoint(ctx, s, domain.ProcessDetectIssues, string(i), next); err != nil {
			return started, notified, err
		}
	}
	tracker, next, err := u.detectBaseline(ctx, s, cps.Get(domain.ProcessDetectIssuesBaseline, domain.ServiceKey), now)
	if err != nil {
		return started, notified, err
	}
	if err := u.finish(ctx, s, tracker); err != nil {
		return started, notified, err
	}
	open[domain.ServiceKey] = tracker.Open()
	notified += u.notify(ctx, s, tracker, open, domain.ServiceKey)
	started += len(tracker.Started())
	next.Open = tracker.Open()
	return started, notified, u.checkpoints.UpdateCheckpoint(ctx, s, domain.ProcessDetectIssuesBaseline, domain.ServiceKey, next)
}

type judgement struct {
	at       time.Time
	judges   []domain.Judge
	findings []domain.Finding
}

// detectInstance はチェックポイントの続きのチャンク・リーク判定・スナップショットを、時刻の順に判定する
func (u *DetectIssuesUsecase) detectInstance(ctx context.Context, s observation.ServiceName, i observation.InstanceName, cp domain.Checkpoint) (*domain.Tracker, domain.Checkpoint, error) {
	tracker := domain.NewTracker(cp.Open)
	h, arrival, err := arrivalOf(ctx, u.holdings, u.lifecycle, s, i)
	if err != nil || h.Latest.IsZero() {
		return tracker, cp, err
	}
	from := startOf(cp, h, detectSpanPerExec)
	to := minTime(h.Latest, from.Add(detectSpanPerExec))
	chunks, err := u.traces.ListTraceChunks(ctx, s, i, from, to)
	if err != nil {
		return nil, cp, err
	}
	checks, err := u.leakChecks.ListLeakChecks(ctx, s, i, to.Add(-time.Hour), to)
	if err != nil {
		return nil, cp, err
	}
	snapshots, err := u.snapshots.ListSnapshots(ctx, s, i, to.Add(-leakGrowthWindow), to)
	if err != nil {
		return nil, cp, err
	}
	var js []judgement
	next := cp
	for _, c := range chunks {
		if c.StartedAt.After(cp.At) {
			js = append(js, judgement{at: c.StartedAt, judges: []domain.Judge{domain.JudgeByThreshold}, findings: domain.JudgeChunk(s, c)})
			next.At = maxTime(next.At, c.StartedAt)
		}
	}
	supported := false
	for _, lc := range checks {
		supported = supported || lc.Status == observation.LeakCheckDone
		if !lc.At.After(cp.At) {
			continue
		}
		snap, ok := domain.NearestSnapshot(snapshots, lc.At)
		js = append(js, judgement{at: lc.At, judges: []domain.Judge{domain.JudgeByLeakCheck}, findings: domain.JudgeLeakCheck(lc, snap, ok)})
		next.At = maxTime(next.At, lc.At)
	}
	if n := len(snapshots); n > 0 && !supported && snapshots[n-1].At.After(cp.At) {
		js = append(js, judgement{at: snapshots[n-1].At, judges: []domain.Judge{domain.JudgeByLeakGrowth}, findings: domain.JudgeLeakGrowth(snapshots)})
		next.At = maxTime(next.At, snapshots[n-1].At)
	}
	slices.SortStableFunc(js, func(a, b judgement) int { return a.at.Compare(b.at) })
	inst := observation.Instance{Service: s, Name: i, Incarnation: incarnationOf(ctx, u.lifecycle, s, i)}
	for _, j := range js {
		tracker.Observe(j.at, inst, j.judges, j.findings)
	}
	if arrival.Ended && !next.At.Before(h.TraceEnd.Add(-time.Minute)) {
		tracker.End(h.Latest)
	}
	if err := u.preserve(ctx, s, tracker); err != nil {
		return nil, cp, err
	}
	return tracker, next, nil
}

func incarnationOf(ctx context.Context, lifecycle domain.LifecycleAccessor, s observation.ServiceName, i observation.InstanceName) observation.Incarnation {
	last, ok, err := lifecycle.LastLifecycle(ctx, s, i)
	if err != nil || !ok {
		return ""
	}
	return last.Incarnation
}

// detectBaseline はサービスのメトリクスを 5 分ごとにまとめ、過去の同じ曜日・時刻と比べる
func (u *DetectIssuesUsecase) detectBaseline(ctx context.Context, s observation.ServiceName, cp domain.Checkpoint, now time.Time) (*domain.Tracker, domain.Checkpoint, error) {
	tracker := domain.NewTracker(cp.Open)
	next := cp
	start := cp.At.Add(domain.BaselineStep)
	if cp.At.IsZero() {
		start = now.Add(-30 * time.Minute).UTC().Truncate(domain.BaselineStep)
	}
	for n, w := 0, start; n < baselineWindowsPerRun && !w.Add(domain.BaselineStep).After(now.Add(-baselineLag)); n, w = n+1, w.Add(domain.BaselineStep) {
		current, err := u.metrics.ListMetrics(ctx, s, w, w.Add(domain.BaselineStep-time.Nanosecond))
		if err != nil {
			return nil, cp, err
		}
		next.At = w
		if len(current) == 0 {
			continue
		}
		var reference [][]domain.MetricSample
		for _, week := range domain.ReferenceWindows(w) {
			samples, err := u.metrics.ListMetrics(ctx, s, week[0], week[len(week)-1].Add(domain.BaselineStep-time.Nanosecond))
			if err != nil {
				return nil, cp, err
			}
			var values []domain.MetricSample
			for _, ws := range week {
				var in []domain.MetricSample
				for _, m := range samples {
					if inStep(m.At, ws, ws.Add(domain.BaselineStep)) {
						in = append(in, m)
					}
				}
				if len(in) > 0 {
					values = append(values, domain.ServiceSample(ws, in))
				}
			}
			reference = append(reference, values)
		}
		findings := domain.JudgeBaseline(s, domain.ServiceSample(w, current), reference)
		tracker.Observe(w, observation.Instance{Service: s, Name: busiest(current)}, []domain.Judge{domain.JudgeByBaseline}, findings)
	}
	if err := u.preserve(ctx, s, tracker); err != nil {
		return nil, cp, err
	}
	return tracker, next, nil
}

// busiest は普段との比較の発生を付けるインスタンス。サービスの値なので、一番長く trace を取っていたインスタンスにする
func busiest(samples []domain.MetricSample) observation.InstanceName {
	per := map[observation.InstanceName]time.Duration{}
	for _, m := range samples {
		per[m.Instance] += m.Traced + time.Duration(m.Logs)
	}
	var (
		best observation.InstanceName
		max  time.Duration = -1
	)
	for i, d := range per {
		if d > max || (d == max && i < best) {
			best, max = i, d
		}
	}
	return best
}

// preserve は発生の前後の execution trace がそろったものを残す。trace が届かないまま待つのをやめたものは、残さずに印だけ付ける
func (u *DetectIssuesUsecase) preserve(ctx context.Context, s observation.ServiceName, tracker *domain.Tracker) error {
	for _, o := range tracker.Pending() {
		i := o.Occurrence.Instance
		_, arrival, err := arrivalOf(ctx, u.holdings, u.lifecycle, s, i)
		if err != nil {
			return err
		}
		from, to := o.Occurrence.Around()
		if !arrival.MaterialReady(to) {
			continue
		}
		chunks, err := u.traces.ListTraceChunks(ctx, s, i, from, to)
		if err != nil {
			return err
		}
		joined, ok := observation.Join(chunks, from, to)
		if !ok {
			tracker.TraceSaved(o, "")
			continue
		}
		p := domain.PreservedTrace{Kind: domain.PreservedAroundIssue, Dir: o.Issue, At: o.Occurrence.StartedAt, From: joined.StartedAt, To: joined.EndedAt()}
		if err := u.preserved.SavePreservedTrace(ctx, s, i, p, joined); err != nil {
			return err
		}
		tracker.TraceSaved(o, p.RelativePath())
	}
	return nil
}

// finish は変わった発生を、発生が始まった日の Issue のファイルに書く
func (u *DetectIssuesUsecase) finish(ctx context.Context, s observation.ServiceName, tracker *domain.Tracker) error {
	type fileKey struct {
		day  time.Time
		name string
	}
	byFile := map[fileKey][]domain.TrackedOccurrence{}
	var keys []fileKey
	for _, o := range tracker.Changed() {
		k := fileKey{domain.IssueDay(o.Occurrence), o.Issue}
		if _, ok := byFile[k]; !ok {
			keys = append(keys, k)
		}
		byFile[k] = append(byFile[k], o)
	}
	slices.SortFunc(keys, func(a, b fileKey) int { return cmp.Or(a.day.Compare(b.day), cmp.Compare(a.name, b.name)) })
	for _, k := range keys {
		is, ok, err := u.issues.ReadIssue(ctx, s, k.day, k.name)
		if err != nil {
			return err
		}
		if !ok {
			first := byFile[k][0]
			is = domain.Issue{Name: first.Issue, Kind: first.Kind, Subject: first.Subject}
		}
		for _, o := range byFile[k] {
			is.Upsert(o.Occurrence)
		}
		if err := u.issues.WriteIssue(ctx, s, k.day, is); err != nil {
			return err
		}
	}
	return nil
}

// notify は、どのインスタンスでも続いていなかった Issue の発生が始まったら知らせる。送れなくても判定は進める
func (u *DetectIssuesUsecase) notify(ctx context.Context, s observation.ServiceName, tracker *domain.Tracker, open map[string][]domain.TrackedOccurrence, self string) int {
	var others []domain.TrackedOccurrence
	for k, os := range open {
		if k != self {
			others = append(others, os...)
		}
	}
	others = append(others, tracker.Open()...)
	sent := 0
	for _, o := range tracker.Started() {
		if !domain.ShouldNotify(o, others) {
			continue
		}
		n := domain.NewNotification(s, o, u.gui)
		for _, dest := range u.notifications[s] {
			client, ok := u.senders[dest.Kind]
			if !ok || client == nil {
				slog.WarnContext(ctx, "unknown notification destination", slog.String("kind", string(dest.Kind)))
				continue
			}
			if err := client.Send(ctx, dest, n); err != nil {
				slog.WarnContext(ctx, "send a notification", slog.String("service", string(s)), slog.String("issue", o.Issue), slog.Any("err", err))
				continue
			}
			sent++
		}
	}
	return sent
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
