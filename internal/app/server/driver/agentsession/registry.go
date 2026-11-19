// Package agentsession は server の側で、つながってきた agent ごとの Session を持つ。
// agent からの接続 (agentlistener) と画面・API のリクエスト (handler) を、その agent の Session に振り分ける
package agentsession

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"github.com/kakkky/gorviz/internal/app/protocol/agentrpc"
	"github.com/kakkky/gorviz/internal/app/server/driver/agentlistener"
	"github.com/kakkky/gorviz/internal/app/server/presentation/handler"
)

// Session は agent 1 つ分。agent が切れても、取ってきたログと保存した録画は残る
type Session struct {
	// Handler は画面・API の向け先
	Handler handler.Session
	// Endpoint は agent からの接続の受け口
	Endpoint agentlistener.Endpoint
	// Workers はその agent のために裏で回し続けるもの。Session を作ったときに 1 回だけ回し始める
	Workers []func(context.Context) error
}

// Registry は server がつながった agent の Session を名前で持つ (handler.Sessions)
type Registry struct {
	newSession func(agentrpc.Hello) (*Session, error)
	mu         sync.RWMutex
	sessions   map[string]*Session
	order      []string
}

// NewRegistry の newSession は、初めてつながった agent の Session を組み立てる
func NewRegistry(newSession func(agentrpc.Hello) (*Session, error)) *Registry {
	return &Registry{newSession: newSession, sessions: map[string]*Session{}}
}

// Attach は agent が push・pull のどちらかの接続を張ってきたときに呼び、その agent の受け口を返す。
// 初めての agent なら Session を作って start で回し始める。同じ名前の agent がつなぎ直したなら、前と同じ Session を使う
func (r *Registry) Attach(ctx context.Context, hello agentrpc.Hello, start func(func(context.Context) error)) (agentlistener.Endpoint, bool) {
	r.mu.Lock()
	s, ok := r.sessions[hello.Name]
	if !ok {
		var err error
		if s, err = r.newSession(hello); err != nil {
			r.mu.Unlock()
			slog.ErrorContext(ctx, "create a session for the agent", slog.String("agent", hello.Name), slog.Any("err", err))
			return agentlistener.Endpoint{}, false
		}
		r.sessions[hello.Name] = s
		r.order = append(r.order, hello.Name)
	}
	r.mu.Unlock()
	slog.InfoContext(ctx, "agent connected", slog.String("agent", hello.Name), slog.String("target", hello.Target), slog.String("kind", string(hello.Kind)))
	if !ok {
		for _, w := range s.Workers {
			start(w)
		}
	}
	return s.Endpoint, true
}

func (r *Registry) Get(name string) (handler.Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[name]
	if !ok {
		return handler.Session{}, false
	}
	return s.Handler, true
}

// Default は名前の指定がないときに使う、最初につながった agent
func (r *Registry) Default() (handler.Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.order) == 0 {
		return handler.Session{}, false
	}
	return r.sessions[r.order[0]].Handler, true
}

func (r *Registry) List() []handler.Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]handler.Session, 0, len(r.order))
	for _, name := range slices.Clone(r.order) {
		out = append(out, r.sessions[name].Handler)
	}
	return out
}
