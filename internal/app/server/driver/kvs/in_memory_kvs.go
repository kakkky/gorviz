package kvs

import "sync"

// InMemoryKVS はセッションのデータをすべてメモリに持つ。server を止めると消える。排他はここで閉じ、f の中で読み書きさせる
type InMemoryKVS struct {
	mu     sync.RWMutex
	values map[string]any
}

func NewInMemoryKVS() *InMemoryKVS {
	return &InMemoryKVS{values: map[string]any{}}
}

// Update は key の値を f で書き換える。値がなければ init で作る
func (k *InMemoryKVS) Update(key string, init func() any, f func(v any)) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.values[key]
	if !ok {
		v = init()
		k.values[key] = v
	}
	f(v)
}

func (k *InMemoryKVS) View(key string, init func() any, f func(v any)) {
	k.mu.RLock()
	v, ok := k.values[key]
	k.mu.RUnlock()
	if !ok {
		k.Update(key, init, func(any) {})
		k.mu.RLock()
		v = k.values[key]
		k.mu.RUnlock()
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	f(v)
}
