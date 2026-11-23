// Package kvs はプロセスのメモリに置くキーと値の置き場
package kvs

import "sync"

type InMemoryKVS struct {
	mu sync.Mutex
	m  map[string]any
}

func NewInMemoryKVS() *InMemoryKVS {
	return &InMemoryKVS{m: map[string]any{}}
}

func (k *InMemoryKVS) Get(key string) (any, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	return v, ok
}

func (k *InMemoryKVS) Set(key string, v any) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = v
}

func (k *InMemoryKVS) Delete(key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.m, key)
}
