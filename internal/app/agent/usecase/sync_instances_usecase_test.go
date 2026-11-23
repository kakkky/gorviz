package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/testhelper/mock"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestSyncInstancesUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantErr := errors.New("boom")
	self := domain.TargetInstance{ID: "self", Instance: observation.Instance{Service: "s", Name: "host", Incarnation: "1"}, PprofAddr: "localhost:6060"}
	pod := func(uid, container string) domain.Pod {
		return domain.Pod{
			UID: uid, Namespace: "ns", Name: "p-" + uid, IP: "10.0.0.1", Running: true,
			Labels:       map[string]string{"app": "api"},
			Annotations:  map[string]string{domain.AnnotationPprofPort: "6060"},
			Containers:   []string{"app"},
			ContainerIDs: map[string]string{"app": container},
		}
	}
	podTarget := func(uid, container string) domain.TargetInstance {
		return domain.TargetInstance{
			ID:        domain.TargetID(uid),
			Instance:  observation.Instance{Service: "api", Name: observation.InstanceName("ns/p-" + uid), Incarnation: observation.Incarnation(container)},
			PprofAddr: "10.0.0.1:6060",
			LogDir:    "/var/log/pods/ns_p-" + uid + "_" + uid + "/app",
		}
	}
	tests := []struct {
		name      string
		input     usecase.SyncInstancesUsecaseInput
		current   []domain.TargetInstance
		mock      func(k *mock.MockKubernetesClient)
		want      usecase.SyncInstancesUsecaseOutput
		wantEnded []observation.Instance
		wantErr   error
	}{
		{
			name:  "observes the configured app in the sidecar mode",
			input: usecase.SyncInstancesUsecaseInput{Mode: domain.ModeSidecar, Self: self},
			mock:  func(*mock.MockKubernetesClient) {},
			want:  usecase.SyncInstancesUsecaseOutput{Targets: []domain.TargetInstance{self}},
		},
		{
			name:    "follows the pods on the node and ends the ones gone or restarted in the daemon mode",
			input:   usecase.SyncInstancesUsecaseInput{Mode: domain.ModeDaemon, PodLogDir: "/var/log/pods"},
			current: []domain.TargetInstance{podTarget("a", "c1"), podTarget("b", "c1")},
			mock: func(k *mock.MockKubernetesClient) {
				k.EXPECT().ListNodePods(ctx).Return([]domain.Pod{pod("a", "c2"), pod("c", "c1")}, nil)
			},
			want: usecase.SyncInstancesUsecaseOutput{
				Targets: []domain.TargetInstance{podTarget("a", "c2"), podTarget("c", "c1")},
				Ended:   []observation.Instance{podTarget("a", "c1").Instance, podTarget("b", "c1").Instance},
			},
			wantEnded: []observation.Instance{podTarget("a", "c1").Instance, podTarget("b", "c1").Instance},
		},
		{
			name:    "keeps the list when the Kubernetes API fails",
			input:   usecase.SyncInstancesUsecaseInput{Mode: domain.ModeDaemon},
			current: []domain.TargetInstance{podTarget("a", "c1")},
			mock: func(k *mock.MockKubernetesClient) {
				k.EXPECT().ListNodePods(ctx).Return(nil, wantErr)
			},
			wantErr: wantErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			k := mock.NewMockKubernetesClient(ctrl)
			tt.mock(k)
			store, _ := newStores(t)
			require.NoError(t, store.UpdateTargets(ctx, func([]domain.TargetInstance) []domain.TargetInstance { return tt.current }))

			uc := usecase.NewUseCase(store, store, nil, nil, nil, store, nil, nil, k, nil).NewSyncInstancesUsecase()
			out, err := uc.Exec(ctx, tt.input)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, usecase.SyncInstancesUsecaseOutput{}, out)
				targets, err := store.Targets(ctx)
				require.NoError(t, err)
				assert.Equal(t, tt.current, targets)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
			targets, err := store.Targets(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want.Targets, targets)
			ended, err := store.EndedInstances(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantEnded, ended)
		})
	}
}
