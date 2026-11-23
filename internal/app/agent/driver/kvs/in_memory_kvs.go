package kvs

import "sync"

// InMemoryKVS は agent のプロセスの中だけで覚えておく状態の置き場。agent が再起動したら消える
type InMemoryKVS struct {
	mu     sync.Mutex
	values map[string]any
}

func NewInMemoryKVS() *InMemoryKVS {
	return &InMemoryKVS{values: map[string]any{}}
}

func (k *InMemoryKVS) Get(key string) (any, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.values[key]
	return v, ok
}

func (k *InMemoryKVS) Set(key string, v any) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.values[key] = v
}

func (k *InMemoryKVS) Delete(key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.values, key)
}

// Update は読んでから書くまでを、ほかの読み書きと重ねずに行う。update が false を返したら消す
func (k *InMemoryKVS) Update(key string, update func(v any, ok bool) (any, bool)) {
	k.mu.Lock()
	defer k.mu.Unlock()
	old, ok := k.values[key]
	v, keep := update(old, ok)
	if !keep {
		delete(k.values, key)
		return
	}
	k.values[key] = v
}
