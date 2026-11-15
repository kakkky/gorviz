package kvs

import "sync"

type InMemoryKVS struct {
	m sync.Map
}

func NewInMemoryKVS() *InMemoryKVS {
	return &InMemoryKVS{}
}

func (k *InMemoryKVS) Get(key string) (any, bool) {
	return k.m.Load(key)
}

func (k *InMemoryKVS) Set(key string, value any) {
	k.m.Store(key, value)
}
