// Package session は server の側で、つながってきた agent ごとの診断・保存・画面をまとめる
package session

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/rpc"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/kakkky/gorviz/adapter/agentrpc"
	"github.com/kakkky/gorviz/adapter/client"
	"github.com/kakkky/gorviz/adapter/kvs"
	"github.com/kakkky/gorviz/adapter/recording"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/presentation/handler"
	"github.com/kakkky/gorviz/usecase"
)

// Session は agent 1 つ分。agent が切れても、取ってきたログと保存した録画は残る
type Session struct {
	Name    string
	Target  string
	Client  *agentrpc.Client
	UseCase *usecase.UseCase
	Handler http.Handler
	refresh time.Duration
}

func newSession(cfg *config.Config, hello agentrpc.Hello) (*Session, error) {
	saveDir := ""
	if cfg.SaveDir != "" {
		saveDir = filepath.Join(cfg.SaveDir, hello.Name)
	}
	saved, err := recording.NewSavedStore(saveDir, client.ParseTrace)
	if err != nil {
		return nil, err
	}
	agent := agentrpc.NewClient()
	sessionCfg := *cfg
	sessionCfg.TargetURL = hello.Name + " — " + hello.Target
	uc := usecase.NewServerUseCase(&sessionCfg, agent, agent, kvs.NewInMemoryKVS(), agent.SchedTrace(),
		recording.NewCompositeTraceRecording(agent, saved), recording.NewLogStore(cfg), saved)
	return &Session{
		Name: hello.Name, Target: hello.Target, Client: agent, UseCase: uc,
		Handler: newMux(handler.NewHandler(&sessionCfg, uc)),
		refresh: cfg.Refresh,
	}, nil
}

// run は refresh ごとに、agent から軽いもの (ログ・dump) を取ってきて判定し、Issue があれば前後の録画を保存する
func (s *Session) run(ctx context.Context) error {
	var (
		syncLogs = s.UseCase.NewSyncLogsUsecase()
		snapshot = s.UseCase.NewRecordRuntimeSnapshotUsecase()
		save     = s.UseCase.NewSaveRecordingsUsecase()
		ticker   = time.NewTicker(s.refresh)
	)
	defer ticker.Stop()
	for {
		if s.Client.Connected() {
			now := time.Now()
			if _, err := syncLogs.Exec(ctx, usecase.SyncLogsUsecaseInput{}); err != nil {
				slog.WarnContext(ctx, "sync logs", slog.String("agent", s.Name), slog.Any("err", err))
			}
			if _, err := snapshot.Exec(ctx, usecase.RecordRuntimeSnapshotUsecaseInput{Now: now}); err != nil && !errors.Is(err, domain.ErrNotFound) {
				slog.WarnContext(ctx, "record runtime snapshot", slog.String("agent", s.Name), slog.Any("err", err))
			}
			if out, err := save.Exec(ctx, usecase.SaveRecordingsUsecaseInput{Now: now}); err != nil {
				slog.WarnContext(ctx, "save recordings", slog.String("agent", s.Name), slog.Any("err", err))
			} else {
				for _, r := range out.Saved {
					slog.InfoContext(ctx, "saved a recording", slog.String("agent", s.Name), slog.String("id", r.ID), slog.Int("chunks", r.Chunks))
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Registry は server がつながった agent を名前で持つ
type Registry struct {
	cfg      *config.Config
	mu       sync.RWMutex
	sessions map[string]*Session
	order    []string
}

func NewRegistry(cfg *config.Config) *Registry {
	return &Registry{cfg: cfg, sessions: map[string]*Session{}}
}

// Attach は agent がつないできたときに呼ぶ。同じ名前の agent がつなぎ直したなら接続だけを差し替え、
// 初めての agent なら Session を作って start で回し始める
func (r *Registry) Attach(ctx context.Context, hello agentrpc.Hello, conn *rpc.Client, start func(func(context.Context) error)) {
	r.mu.Lock()
	s, ok := r.sessions[hello.Name]
	if !ok {
		var err error
		if s, err = newSession(r.cfg, hello); err != nil {
			r.mu.Unlock()
			slog.ErrorContext(ctx, "create a session for the agent", slog.String("agent", hello.Name), slog.Any("err", err))
			conn.Close()
			return
		}
		r.sessions[hello.Name] = s
		r.order = append(r.order, hello.Name)
	}
	r.mu.Unlock()
	s.Client.SetConn(conn)
	slog.InfoContext(ctx, "agent connected", slog.String("agent", hello.Name), slog.String("target", hello.Target))
	if !ok {
		start(s.run)
	}
}

func (r *Registry) Get(name string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[name]
	return s, ok
}

// Default は名前の指定がないときに使う、最初につながった agent
func (r *Registry) Default() (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.order) == 0 {
		return nil, false
	}
	return r.sessions[r.order[0]], true
}

func (r *Registry) List() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Session, 0, len(r.order))
	for _, name := range slices.Clone(r.order) {
		out = append(out, r.sessions[name])
	}
	return out
}

func newMux(h *handler.Handler) http.Handler {
	mux := http.NewServeMux()
	for _, route := range h.Routes() {
		mux.Handle(route.Pattern, route.Handler)
	}
	return mux
}
