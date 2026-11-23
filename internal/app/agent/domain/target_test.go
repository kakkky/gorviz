package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestTargetsFromPods(t *testing.T) {
	t.Parallel()
	pod := domain.Pod{
		UID: "uid-1", Namespace: "shop", Name: "checkout-abc", IP: "10.0.0.5", Running: true,
		Labels:       map[string]string{"app": "checkout"},
		Annotations:  map[string]string{domain.AnnotationPprofPort: "6060", domain.AnnotationLogContainer: "app"},
		Containers:   []string{"envoy", "app"},
		ContainerIDs: map[string]string{"app": "containerd://aaa", "envoy": "containerd://bbb"},
	}
	unmarked := pod
	unmarked.UID, unmarked.Annotations = "uid-2", nil
	pending := pod
	pending.UID, pending.Running = "uid-3", false
	tests := []struct {
		name string
		pods []domain.Pod
		want []domain.TargetInstance
	}{
		{
			name: "observes a running pod with the annotation through its log container",
			pods: []domain.Pod{pod},
			want: []domain.TargetInstance{{
				ID:        "uid-1",
				Instance:  observation.Instance{Service: "checkout", Name: "shop/checkout-abc", Incarnation: "containerd://aaa"},
				PprofAddr: "10.0.0.5:6060",
				LogDir:    "/var/log/pods/shop_checkout-abc_uid-1/app",
			}},
		},
		{
			name: "skips pods without the annotation and pods not running",
			pods: []domain.Pod{unmarked, pending},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.TargetsFromPods(tt.pods, "/var/log/pods"))
		})
	}
}

func TestSyncTargets(t *testing.T) {
	t.Parallel()
	a1 := domain.TargetInstance{ID: "a", Instance: observation.Instance{Service: "s", Name: "a", Incarnation: "c1"}}
	a2 := domain.TargetInstance{ID: "a", Instance: observation.Instance{Service: "s", Name: "a", Incarnation: "c2"}}
	b := domain.TargetInstance{ID: "b", Instance: observation.Instance{Service: "s", Name: "b", Incarnation: "c1"}}
	self := domain.TargetInstance{ID: "self", Instance: observation.Instance{Service: "s", Name: "x", Incarnation: "1"}}
	selfRestarted := self
	selfRestarted.Instance.Incarnation = "1794713392000000000"
	tests := []struct {
		name      string
		mode      domain.Mode
		current   []domain.TargetInstance
		found     []domain.TargetInstance
		wantNext  []domain.TargetInstance
		wantEnded []observation.Instance
	}{
		{
			name:     "adds new targets",
			mode:     domain.ModeDaemon,
			found:    []domain.TargetInstance{a1, b},
			wantNext: []domain.TargetInstance{a1, b},
		},
		{
			name:      "ends targets that disappeared",
			mode:      domain.ModeDaemon,
			current:   []domain.TargetInstance{a1, b},
			found:     []domain.TargetInstance{a1},
			wantNext:  []domain.TargetInstance{a1},
			wantEnded: []observation.Instance{b.Instance},
		},
		{
			name:      "ends the previous instance when the container changes",
			mode:      domain.ModeDaemon,
			current:   []domain.TargetInstance{a1},
			found:     []domain.TargetInstance{a2},
			wantNext:  []domain.TargetInstance{a2},
			wantEnded: []observation.Instance{a1.Instance},
		},
		{
			name:     "keeps the incarnation the sidecar agent detected",
			mode:     domain.ModeSidecar,
			current:  []domain.TargetInstance{selfRestarted},
			found:    []domain.TargetInstance{self},
			wantNext: []domain.TargetInstance{selfRestarted},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next, ended := domain.SyncTargets(tt.current, tt.found, tt.mode)
			assert.Equal(t, tt.wantNext, next)
			assert.Equal(t, tt.wantEnded, ended)
		})
	}
}

func TestProcessReplaced(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		mode         domain.Mode
		prevMaxAlive observation.GoroutineID
		curMax       observation.GoroutineID
		want         bool
	}{
		{name: "sees a new process when the chunk max falls below the goroutines alive before", mode: domain.ModeSidecar, prevMaxAlive: 500, curMax: 40, want: true},
		{name: "keeps the process while the max goroutine ID grows", mode: domain.ModeSidecar, prevMaxAlive: 500, curMax: 520},
		{name: "keeps the process on the first chunk", mode: domain.ModeSidecar, curMax: 40},
		{name: "leaves daemon pods to the container ID", mode: domain.ModeDaemon, prevMaxAlive: 500, curMax: 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.ProcessReplaced(tt.mode, tt.prevMaxAlive, tt.curMax))
		})
	}
}

func TestMaxAliveGoroutineID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		parsed domain.ParsedTrace
		want   observation.GoroutineID
	}{
		{
			name:   "ignores goroutines that ended in the chunk",
			parsed: domain.ParsedTrace{MaxGoroutineID: 900, Goroutines: map[observation.GoroutineID]domain.GoroutineAtEnd{1: {Alive: true}, 40: {Alive: true}, 900: {}}},
			want:   40,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.MaxAliveGoroutineID(tt.parsed))
		})
	}
}

func TestSelfTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mode domain.Mode
		want observation.Incarnation
	}{
		{name: "starts the sidecar app at a fixed incarnation so an agent restart keeps the instance", mode: domain.ModeSidecar, want: "1"},
		{name: "uses the agent ID for the library type because the agent restarts with the app", mode: domain.ModeLibrary, want: "agent-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.SelfTarget(tt.mode, "svc", "host", "localhost:6060", "agent-1")
			assert.Equal(t, tt.want, got.Instance.Incarnation)
		})
	}
}

func TestResolveLogTarget(t *testing.T) {
	t.Parallel()
	podA := domain.TargetInstance{ID: "a", LogDir: "/var/log/pods/ns_a_1/app"}
	podB := domain.TargetInstance{ID: "b", LogDir: "/var/log/pods/ns_b_2/app"}
	self := domain.TargetInstance{ID: "self"}
	tests := []struct {
		name    string
		targets []domain.TargetInstance
		source  observation.LogSource
		path    string
		want    domain.TargetID
		wantOK  bool
	}{
		{name: "matches a pod log file by its directory", targets: []domain.TargetInstance{podA, podB}, source: observation.LogSourceFile, path: "/var/log/pods/ns_b_2/app/0.log", want: "b", wantOK: true},
		{name: "drops a file of a pod that is not observed", targets: []domain.TargetInstance{podA, podB}, source: observation.LogSourceFile, path: "/var/log/pods/ns_c_3/app/0.log"},
		{name: "gives every line to the only app", targets: []domain.TargetInstance{self}, source: observation.LogSourceStdin, want: "self", wantOK: true},
		{name: "drops lines while no app is known", source: observation.LogSourceTCP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.ResolveLogTarget(tt.targets, tt.source, tt.path)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got.ID)
		})
	}
}
