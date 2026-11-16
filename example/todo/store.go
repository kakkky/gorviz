package main

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"
)

type Todo struct {
	ID        int
	Title     string
	Done      bool
	Reminder  bool
	CreatedAt time.Time
	DoneAt    time.Time
}

// Store はメモリに持つ Todo の置き場。読み書きのたびに DB の往復に見立てた数 ms の遅延を入れる
type Store struct {
	mu     sync.Mutex
	todos  map[int]Todo
	nextID int
}

func NewStore() *Store {
	return &Store{todos: map[int]Todo{}, nextID: 1}
}

func dbLatency() {
	time.Sleep(time.Duration(2+rand.IntN(6)) * time.Millisecond)
}

func (s *Store) List(query string) []Todo {
	s.mu.Lock()
	defer s.mu.Unlock()
	dbLatency()
	out := make([]Todo, 0, len(s.todos))
	for _, t := range s.todos {
		if query == "" || strings.Contains(strings.ToLower(t.Title), strings.ToLower(query)) {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b Todo) int {
		if a.Done != b.Done {
			if a.Done {
				return 1
			}
			return -1
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return out
}

func (s *Store) Add(title string, reminder bool) Todo {
	s.mu.Lock()
	defer s.mu.Unlock()
	dbLatency()
	t := Todo{ID: s.nextID, Title: title, Reminder: reminder, CreatedAt: time.Now()}
	s.todos[t.ID] = t
	s.nextID++
	return t
}

func (s *Store) Complete(id int) (Todo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dbLatency()
	t, ok := s.todos[id]
	if !ok {
		return Todo{}, false
	}
	t.Done, t.DoneAt = true, time.Now()
	s.todos[id] = t
	return t, true
}

func (s *Store) Delete(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dbLatency()
	delete(s.todos, id)
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.todos)
}

var sampleTitles = []string{
	"牛乳を買う", "請求書を払う", "週報を書く", "歯医者を予約する", "PR をレビューする", "ジムに行く",
	"部屋を掃除する", "本を返す", "母に電話する", "旅行の宿を取る", "障害報告を書く", "依存ライブラリを更新する",
}

// Seed はサンプルの todo を n 件入れる
func (s *Store) Seed(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for range n {
		t := Todo{ID: s.nextID, Title: fmt.Sprintf("%s #%d", sampleTitles[rand.IntN(len(sampleTitles))], s.nextID), CreatedAt: time.Now()}
		t.Done = rand.IntN(3) == 0
		s.todos[t.ID] = t
		s.nextID++
	}
}
