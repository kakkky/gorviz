package accessor

import (
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
)

const sessionKey = "session"

type SessionAccessor struct {
	kvs *kvs.InMemoryKVS
}

func NewSessionAccessor(k *kvs.InMemoryKVS) *SessionAccessor {
	return &SessionAccessor{kvs: k}
}

func (a *SessionAccessor) Session() domain.Session {
	v, _ := a.kvs.Get(sessionKey)
	s, _ := v.(domain.Session)
	return s
}

func (a *SessionAccessor) UpdateSession(f func(s *domain.Session)) {
	a.kvs.Update(sessionKey, func(v any) any {
		s, _ := v.(domain.Session)
		f(&s)
		return s
	})
}
