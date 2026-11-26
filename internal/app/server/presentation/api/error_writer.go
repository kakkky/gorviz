package api

import (
	"net/http"
	"strconv"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/protocol/server"
)

type errorBody struct {
	Error string   `json:"error"`
	Docs  string   `json:"docs"`
	Next  []string `json:"next,omitempty"`
}

// ErrorWriter は API のエラーを、Accept に応じた形で書く。エラーにはドキュメントを引くサブコマンドを添える
type ErrorWriter struct{}

func NewErrorWriter() *ErrorWriter { return &ErrorWriter{} }

func (ErrorWriter) WriteError(rw http.ResponseWriter, r *http.Request, status int, err error) {
	b, media, encErr := encode(r, errorBody{Error: err.Error(), Docs: "gorviz docs"})
	if encErr != nil {
		http.Error(rw, err.Error(), status)
		return
	}
	rw.Header().Set("Content-Type", media)
	rw.Header().Set(server.HeaderExitCode, strconv.Itoa(domain.ExitNotDiagnosed))
	rw.WriteHeader(status)
	_, _ = rw.Write(b)
}
