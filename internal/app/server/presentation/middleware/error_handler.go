package middleware

import (
	"errors"
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

type errorKey struct{}

type errorSlot struct {
	err error
}

// Fail はリクエストのエラーを載せる。handler はこれを呼んで即 return し、レスポンスは Middleware が書く
func Fail(r *http.Request, err error) {
	if s, ok := r.Context().Value(errorKey{}).(*errorSlot); ok {
		s.err = err
	}
}

// ErrorWriter はエラーの種別を status に変えたあと、返す相手 (画面・API) に合わせて中身を書く
type ErrorWriter interface {
	WriteError(rw http.ResponseWriter, r *http.Request, status int, err error)
}

func NewMiddleware(api, web ErrorWriter) *Middleware {
	return &Middleware{api: api, web: web}
}

func StatusOf(err error) int {
	switch {
	case errors.Is(err, domain.ErrInvalidArgument):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}
