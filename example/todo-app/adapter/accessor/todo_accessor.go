package accessor

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
	"github.com/kakkky/gorviz/example/todo-app/driver/memstore"
)

type TodoAccessor struct {
	store *memstore.MemStore
}

func NewTodoAccessor(store *memstore.MemStore) *TodoAccessor {
	return &TodoAccessor{store: store}
}

func toTodo(r memstore.Row) domain.Todo {
	return domain.Todo{ID: domain.TodoID(r.ID), Title: domain.Title(r.Title), Done: r.Done, Reminder: r.Reminder, CreatedAt: r.CreatedAt, DoneAt: r.DoneAt}
}

func all(t *memstore.Tables) []domain.Todo {
	out := make([]domain.Todo, 0, len(t.Todos))
	for _, r := range t.Todos {
		out = append(out, toTodo(r))
	}
	return out
}

func (a *TodoAccessor) List(ctx context.Context) ([]domain.Todo, error) {
	var out []domain.Todo
	err := a.store.Do(func(t *memstore.Tables) error {
		out = all(t)
		return nil
	})
	return out, err
}

func (a *TodoAccessor) Add(ctx context.Context, title domain.Title, reminder bool, now time.Time) (domain.Todo, error) {
	var out domain.Todo
	err := a.store.Do(func(t *memstore.Tables) error {
		r := memstore.Row{ID: t.NextID, Title: string(title), Reminder: reminder, CreatedAt: now}
		t.Todos[r.ID] = r
		t.NextID++
		out = toTodo(r)
		return nil
	})
	return out, err
}

func (a *TodoAccessor) Complete(ctx context.Context, id domain.TodoID, now time.Time) (domain.Todo, error) {
	var out domain.Todo
	err := a.store.Do(func(t *memstore.Tables) error {
		r, ok := t.Todos[int(id)]
		if !ok {
			return domain.ErrNotFound.With("todo not found")
		}
		r.Done, r.DoneAt = true, now
		t.Todos[r.ID] = r
		out = toTodo(r)
		return nil
	})
	return out, err
}

func (a *TodoAccessor) Delete(ctx context.Context, id domain.TodoID) error {
	return a.store.Do(func(t *memstore.Tables) error {
		delete(t.Todos, int(id))
		return nil
	})
}

func (a *TodoAccessor) Count(ctx context.Context) (int, error) {
	var n int
	err := a.store.Do(func(t *memstore.Tables) error {
		n = len(t.Todos)
		return nil
	})
	return n, err
}

func (a *TodoAccessor) WithAllLocked(ctx context.Context, fn func([]domain.Todo) error) error {
	return a.store.Do(func(t *memstore.Tables) error { return fn(all(t)) })
}
