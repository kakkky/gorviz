package accessor

import (
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
)

type TraceIndexAccessor struct {
	kvs *kvs.InMemoryKVS
}

func NewTraceIndexAccessor(k *kvs.InMemoryKVS) *TraceIndexAccessor {
	return &TraceIndexAccessor{kvs: k}
}

func newTraceIndex() any { return domain.NewTraceIndex() }

func (a *TraceIndexAccessor) UpdateTraceIndex(f func(ix *domain.TraceIndex)) {
	a.kvs.Update(traceIndexKey, newTraceIndex, func(v any) { f(v.(*domain.TraceIndex)) })
}

func (a *TraceIndexAccessor) ViewTraceIndex(f func(ix *domain.TraceIndex)) {
	a.kvs.View(traceIndexKey, newTraceIndex, func(v any) { f(v.(*domain.TraceIndex)) })
}
