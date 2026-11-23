package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/testhelper/mock"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestDetectIssuesUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 11, 21, 14, 10, 0, 0, time.UTC)
	start := now.Add(-time.Minute)
	svc, name := observation.ServiceName("checkout"), observation.InstanceName("checkout-1")
	thread := observation.TraceChunk{StartedAt: start, Duration: 5 * time.Second, Threads: 6000, Stats: observation.TraceStats{Procs: 1}}
	quiet := observation.TraceChunk{StartedAt: start.Add(5 * time.Second), Duration: 5 * time.Second, Threads: 10, Stats: observation.TraceStats{Procs: 1}}
	issueName := domain.IssueName(domain.IssueThreadExplosion, string(svc))
	dest := domain.Destination{Kind: domain.DestinationWebhook, URL: "https://hooks.example/x"}
	otherOngoing := domain.Checkpoints{domain.ProcessDetectIssues: {"checkout-2": {At: start, Open: []domain.TrackedOccurrence{{Issue: issueName,
		Occurrence: domain.Occurrence{Instance: "checkout-2", Judge: domain.JudgeByThreshold, StartedAt: start.Add(-time.Hour)}}}}}}

	type mocks struct {
		inventory   *mock.MockInventoryAccessor
		checkpoints *mock.MockCheckpointAccessor
		holdings    *mock.MockHoldingAccessor
		lifecycle   *mock.MockLifecycleAccessor
		traces      *mock.MockTraceAccessor
		snapshots   *mock.MockSnapshotAccessor
		leakChecks  *mock.MockLeakCheckAccessor
		metrics     *mock.MockMetricAccessor
		issues      *mock.MockIssueAccessor
		preserved   *mock.MockPreservedTraceAccessor
		webhook     *mock.MockNotificationClient
	}
	detect := func(m mocks, cps domain.Checkpoints, chunks []observation.TraceChunk) {
		m.inventory.EXPECT().ListServices(ctx).Return([]observation.ServiceName{svc}, nil)
		m.inventory.EXPECT().ListInstances(ctx, svc).Return([]observation.InstanceName{name}, nil)
		m.checkpoints.EXPECT().ReadCheckpoints(ctx, svc).Return(cps, nil)
		m.holdings.EXPECT().Holding(ctx, svc, name).Return(domain.Holding{TraceEnd: now, Latest: now}, nil).AnyTimes()
		m.lifecycle.EXPECT().LastLifecycle(ctx, svc, name).Return(domain.LifecycleEvent{Instance: name, Incarnation: "inc-1", Kind: domain.LifecycleStart}, true, nil).AnyTimes()
		m.traces.EXPECT().ListTraceChunks(ctx, svc, name, gomock.Any(), gomock.Any()).Return(chunks, nil).AnyTimes()
		m.leakChecks.EXPECT().ListLeakChecks(ctx, svc, name, gomock.Any(), gomock.Any()).Return(nil, nil)
		m.snapshots.EXPECT().ListSnapshots(ctx, svc, name, gomock.Any(), gomock.Any()).Return(nil, nil)
		m.metrics.EXPECT().ListMetrics(ctx, svc, gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		m.checkpoints.EXPECT().UpdateCheckpoint(ctx, svc, domain.ProcessDetectIssuesBaseline, domain.ServiceKey, gomock.Any()).Return(nil)
	}
	tests := []struct {
		name string
		mock func(m mocks)
		want usecase.DetectIssuesUsecaseOutput
	}{
		{
			name: "starts an occurrence, writes it with the preserved trace, and notifies when no instance has it ongoing",
			mock: func(m mocks) {
				detect(m, domain.Checkpoints{}, []observation.TraceChunk{thread, quiet})
				m.preserved.EXPECT().SavePreservedTrace(ctx, svc, name, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ observation.ServiceName, _ observation.InstanceName, p domain.PreservedTrace, _ observation.TraceChunk) error {
					assert.Equal(t, issueName, p.Dir)
					assert.Equal(t, start, p.At)
					return nil
				})
				m.issues.EXPECT().ReadIssue(ctx, svc, domain.IssueDay(domain.Occurrence{StartedAt: start}), issueName).Return(domain.Issue{}, false, nil)
				m.issues.EXPECT().WriteIssue(ctx, svc, gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ observation.ServiceName, _ time.Time, is domain.Issue) error {
					require.Len(t, is.Occurrences, 1)
					o := is.Occurrences[0]
					assert.Equal(t, domain.IssueThreadExplosion, is.Kind)
					assert.Equal(t, start, o.StartedAt)
					assert.Equal(t, start.Add(5*time.Second), o.EndedAt)
					assert.NotEmpty(t, o.TraceFile)
					return nil
				})
				m.webhook.EXPECT().Send(ctx, dest, gomock.Any()).DoAndReturn(func(_ context.Context, _ domain.Destination, n domain.Notification) error {
					assert.Equal(t, issueName, n.Issue)
					assert.Contains(t, n.URL, "/services/checkout/issues/"+issueName)
					return nil
				})
				m.checkpoints.EXPECT().UpdateCheckpoint(ctx, svc, domain.ProcessDetectIssues, string(name), gomock.Any()).DoAndReturn(func(_ context.Context, _ observation.ServiceName, _ domain.Process, _ string, cp domain.Checkpoint) error {
					assert.Equal(t, quiet.StartedAt, cp.At)
					assert.Empty(t, cp.Open)
					return nil
				})
			},
			want: usecase.DetectIssuesUsecaseOutput{Started: 1, Notified: 1},
		},
		{
			name: "does not notify while another instance has the same issue ongoing",
			mock: func(m mocks) {
				detect(m, otherOngoing, []observation.TraceChunk{thread})
				m.preserved.EXPECT().SavePreservedTrace(ctx, svc, name, gomock.Any(), gomock.Any()).Return(nil)
				m.issues.EXPECT().ReadIssue(ctx, svc, gomock.Any(), issueName).Return(domain.Issue{}, false, nil)
				m.issues.EXPECT().WriteIssue(ctx, svc, gomock.Any(), gomock.Any()).Return(nil)
				m.checkpoints.EXPECT().UpdateCheckpoint(ctx, svc, domain.ProcessDetectIssues, string(name), gomock.Any()).DoAndReturn(func(_ context.Context, _ observation.ServiceName, _ domain.Process, _ string, cp domain.Checkpoint) error {
					require.Len(t, cp.Open, 1)
					assert.True(t, cp.Open[0].Occurrence.Ongoing())
					return nil
				})
			},
			want: usecase.DetectIssuesUsecaseOutput{Started: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			m := mocks{inventory: mock.NewMockInventoryAccessor(ctrl), checkpoints: mock.NewMockCheckpointAccessor(ctrl), holdings: mock.NewMockHoldingAccessor(ctrl),
				lifecycle: mock.NewMockLifecycleAccessor(ctrl), traces: mock.NewMockTraceAccessor(ctrl), snapshots: mock.NewMockSnapshotAccessor(ctrl),
				leakChecks: mock.NewMockLeakCheckAccessor(ctrl), metrics: mock.NewMockMetricAccessor(ctrl), issues: mock.NewMockIssueAccessor(ctrl),
				preserved: mock.NewMockPreservedTraceAccessor(ctrl), webhook: mock.NewMockNotificationClient(ctrl)}
			tt.mock(m)

			settings := usecase.Settings{GUI: domain.GUI{Base: "https://gorviz.example"}, Notifications: map[observation.ServiceName][]domain.Destination{svc: {dest}}}
			uc := usecase.NewUseCase(settings, nil, m.holdings, m.inventory, nil, m.traces, m.snapshots, m.leakChecks, m.lifecycle, m.metrics, nil,
				m.issues, m.preserved, nil, m.checkpoints, nil, m.webhook, nil, nil).NewDetectIssuesUsecase()
			out, err := uc.Exec(ctx, usecase.DetectIssuesUsecaseInput{Now: now})

			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
		})
	}
}
