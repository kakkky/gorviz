package accessor

import (
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
)

type SessionAccessor struct {
	kvs *kvs.InMemoryKVS
}

func NewSessionAccessor(k *kvs.InMemoryKVS) *SessionAccessor {
	return &SessionAccessor{kvs: k}
}

func newSession() any { return &domain.Session{} }

func (a *SessionAccessor) Session() domain.Session {
	var s domain.Session
	a.kvs.View(sessionKey, newSession, func(v any) { s = *v.(*domain.Session) })
	return s
}

func (a *SessionAccessor) UpdateSession(f func(s *domain.Session)) {
	a.kvs.Update(sessionKey, newSession, func(v any) { f(v.(*domain.Session)) })
}
