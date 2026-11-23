package ringbuffer

import (
	"errors"
	"slices"
	"sync"

	"github.com/kakkky/gorviz/internal/app/agent/config"
)

// RingBuffer は送るまでのものを、キーごとに積んだ順に持つ。全体の大きさが上限を超えたら、キーを問わず一番古いものから捨てる
type RingBuffer[K comparable] struct {
	mu       sync.Mutex
	maxBytes int
	total    int
	seq      uint64
	queues   map[K][]entry
}

type entry struct {
	seq   uint64
	value any
	size  int
}

func NewRingBuffer[K comparable](cfg *config.Config) (*RingBuffer[K], error) {
	if cfg.BufferBytes <= 0 {
		return nil, errors.New("the ring buffer size must be positive")
	}
	return &RingBuffer[K]{maxBytes: cfg.BufferBytes, queues: map[K][]entry{}}, nil
}

func (b *RingBuffer[K]) Append(key K, value any, size int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	b.queues[key] = append(b.queues[key], entry{seq: b.seq, value: value, size: size})
	b.total += size
	for b.total > b.maxBytes && b.dropOldest() {
	}
}

func (b *RingBuffer[K]) dropOldest() bool {
	var (
		oldest K
		seq    uint64
		found  bool
	)
	for k, q := range b.queues {
		if len(q) > 0 && (!found || q[0].seq < seq) {
			oldest, seq, found = k, q[0].seq, true
		}
	}
	if !found {
		return false
	}
	q := b.queues[oldest]
	b.total -= q[0].size
	q[0] = entry{}
	if len(q) == 1 {
		delete(b.queues, oldest)
	} else {
		b.queues[oldest] = q[1:]
	}
	return true
}

func (b *RingBuffer[K]) Values(key K) []any {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.queues[key]
	out := make([]any, len(q))
	for i, e := range q {
		out[i] = e.value
	}
	return out
}

// RemoveIf は drop が true を返したものを捨て、残りを返す
func (b *RingBuffer[K]) RemoveIf(key K, drop func(v any) bool) []any {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.queues[key]
	kept := make([]entry, 0, len(q))
	for _, e := range q {
		if drop(e.value) {
			b.total -= e.size
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(b.queues, key)
	} else {
		b.queues[key] = kept
	}
	out := make([]any, len(kept))
	for i, e := range kept {
		out[i] = e.value
	}
	return out
}

func (b *RingBuffer[K]) Keys() []K {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]K, 0, len(b.queues))
	for k := range b.queues {
		out = append(out, k)
	}
	return slices.Clip(out)
}

func (b *RingBuffer[K]) Delete(key K) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.queues[key] {
		b.total -= e.size
	}
	delete(b.queues, key)
}
