package kvs

import "sync"

// InMemoryKVS はセッションのデータを、キーごとの値としてメモリに持つ。server を止めると消える。
// 入れた値は書き換えない約束で、Get はその値を返す。排他はここで閉じる
type InMemoryKVS struct {
	mu     sync.RWMutex
	values map[string]any
}

func NewInMemoryKVS() *InMemoryKVS {
	return &InMemoryKVS{values: map[string]any{}}
}

func (k *InMemoryKVS) Get(key string) (any, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	v, ok := k.values[key]
	return v, ok
}

func (k *InMemoryKVS) Set(key string, v any) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.values[key] = v
}

// Update は key の値を f で作り直して入れる。ほかの Update と重ならないので、読んでから書くまでのあいだに書き換えられない。値がなければ f に nil を渡す
func (k *InMemoryKVS) Update(key string, f func(v any) any) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.values[key] = f(k.values[key])
}
