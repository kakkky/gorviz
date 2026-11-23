package accessor

import (
	"context"
	"maps"
	"slices"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/driver/kvs"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const (
	targetsKey = "targets"
	statesKey  = "states"
	endedKey   = "ended"
)

func instanceKey(kind string, in observation.Instance) string {
	return kind + "/" + string(in.Service) + "\x00" + string(in.Name) + "\x00" + string(in.Incarnation)
}

// KVSAccessor は agent が覚えておく状態を、プロセスの中の置き場に読み書きする
type KVSAccessor struct {
	kvs *kvs.InMemoryKVS
}

func NewKVSAccessor(k *kvs.InMemoryKVS) *KVSAccessor {
	return &KVSAccessor{kvs: k}
}

func (a *KVSAccessor) Targets(context.Context) ([]domain.TargetInstance, error) {
	v, _ := a.kvs.Get(targetsKey)
	ts, _ := v.([]domain.TargetInstance)
	return slices.Clone(ts), nil
}

func (a *KVSAccessor) UpdateTargets(_ context.Context, update func([]domain.TargetInstance) []domain.TargetInstance) error {
	a.kvs.Update(targetsKey, func(v any, _ bool) (any, bool) {
		ts, _ := v.([]domain.TargetInstance)
		return slices.Clone(update(slices.Clone(ts))), true
	})
	return nil
}

func (a *KVSAccessor) TargetState(_ context.Context, id domain.TargetID) (domain.TargetState, error) {
	v, _ := a.kvs.Get(statesKey)
	states, _ := v.(map[domain.TargetID]domain.TargetState)
	return states[id], nil
}

func (a *KVSAccessor) SetTargetState(_ context.Context, id domain.TargetID, s domain.TargetState) error {
	a.kvs.Update(statesKey, func(v any, _ bool) (any, bool) {
		states, _ := v.(map[domain.TargetID]domain.TargetState)
		states = maps.Clone(states)
		if states == nil {
			states = map[domain.TargetID]domain.TargetState{}
		}
		states[id] = s
		return states, true
	})
	return nil
}

func (a *KVSAccessor) RetainTargetStates(_ context.Context, ids []domain.TargetID) error {
	a.kvs.Update(statesKey, func(v any, _ bool) (any, bool) {
		states, _ := v.(map[domain.TargetID]domain.TargetState)
		states = maps.Clone(states)
		maps.DeleteFunc(states, func(id domain.TargetID, _ domain.TargetState) bool { return !slices.Contains(ids, id) })
		return states, len(states) > 0
	})
	return nil
}

func (a *KVSAccessor) GoroutineMemory(_ context.Context, in observation.Instance) (domain.GoroutineMemory, error) {
	v, _ := a.kvs.Get(instanceKey("memory", in))
	m, _ := v.(domain.GoroutineMemory)
	return maps.Clone(m), nil
}

func (a *KVSAccessor) SetGoroutineMemory(_ context.Context, in observation.Instance, m domain.GoroutineMemory) error {
	a.kvs.Set(instanceKey("memory", in), maps.Clone(m))
	return nil
}

func (a *KVSAccessor) NextLogSeq(_ context.Context, in observation.Instance) (uint64, error) {
	var n uint64
	a.kvs.Update(instanceKey("logseq", in), func(v any, _ bool) (any, bool) {
		cur, _ := v.(uint64)
		n = cur + 1
		return n, true
	})
	return n, nil
}

func (a *KVSAccessor) Cursor(_ context.Context, in observation.Instance) (observation.Cursor, error) {
	v, _ := a.kvs.Get(instanceKey("cursor", in))
	c, _ := v.(observation.Cursor)
	return c, nil
}

func (a *KVSAccessor) SetCursor(_ context.Context, in observation.Instance, c observation.Cursor) error {
	a.kvs.Set(instanceKey("cursor", in), c)
	return nil
}

func (a *KVSAccessor) MarkEnded(_ context.Context, in observation.Instance) error {
	a.kvs.Update(endedKey, func(v any, _ bool) (any, bool) {
		set, _ := v.(map[observation.Instance]bool)
		set = maps.Clone(set)
		if set == nil {
			set = map[observation.Instance]bool{}
		}
		set[in] = true
		return set, true
	})
	return nil
}

func (a *KVSAccessor) EndedInstances(context.Context) ([]observation.Instance, error) {
	v, _ := a.kvs.Get(endedKey)
	set, _ := v.(map[observation.Instance]bool)
	return slices.Collect(maps.Keys(set)), nil
}

func (a *KVSAccessor) Forget(_ context.Context, in observation.Instance) error {
	a.kvs.Update(endedKey, func(v any, _ bool) (any, bool) {
		set, _ := v.(map[observation.Instance]bool)
		set = maps.Clone(set)
		delete(set, in)
		return set, len(set) > 0
	})
	for _, kind := range []string{"memory", "logseq", "cursor"} {
		a.kvs.Delete(instanceKey(kind, in))
	}
	return nil
}
