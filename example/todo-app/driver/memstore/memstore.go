// Package memstore は Todo をメモリに持つ置き場。DB の代わりなので、触るたびに往復に見立てた数 ms の遅延を入れる
package memstore

import (
	"math/rand/v2"
	"sync"
	"time"
)

type Row struct {
	ID        int
	Title     string
	Done      bool
	Reminder  bool
	CreatedAt time.Time
	DoneAt    time.Time
}

type Tables struct {
	Todos  map[int]Row
	NextID int
}

type MemStore struct {
	mu     sync.Mutex
	tables Tables
}

func NewMemStore() *MemStore {
	return &MemStore{tables: Tables{Todos: map[int]Row{}, NextID: 1}}
}

// Do は fn が返るまで、ほかの Do を待たせる
func (s *MemStore) Do(fn func(*Tables) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	time.Sleep(time.Duration(2+rand.IntN(6)) * time.Millisecond)
	return fn(&s.tables)
}
