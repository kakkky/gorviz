package web

import (
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
)

// ErrorWriter は画面のエラーを、エラーの画面で書く
type ErrorWriter struct{}

func NewErrorWriter() *ErrorWriter { return &ErrorWriter{} }

func (ErrorWriter) WriteError(rw http.ResponseWriter, r *http.Request, status int, err error) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(status)
	_ = pages.Error(status, err.Error()).Render(r.Context(), rw)
}
