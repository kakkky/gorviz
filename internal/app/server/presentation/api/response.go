package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/protocol/server"
)

// pageBudget は一覧を区切る量。件数ではなく返す量で区切り、LLM エージェントのコンテキストを埋めないようにする
const pageBudget = 6 << 10

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), server.MediaJSON)
}

func encode(r *http.Request, v any) ([]byte, string, error) {
	if wantsJSON(r) {
		b, err := json.MarshalIndent(v, "", "  ")
		return append(b, '\n'), server.MediaJSON, err
	}
	b, err := marshalTOON(v)
	return b, server.MediaTOON + "; charset=utf-8", err
}

// write は Accept に応じた形 (TOON / JSON) で書く
func write(rw http.ResponseWriter, r *http.Request, v any, exitCode int) error {
	b, media, err := encode(r, v)
	if err != nil {
		return domain.ErrInternal.Wrap(err, "encode response")
	}
	rw.Header().Set("Content-Type", media)
	rw.Header().Set(server.HeaderExitCode, strconv.Itoa(exitCode))
	_, _ = rw.Write(b)
	return nil
}

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

// at はセッションの始まりからの経過時間。gorviz goroutine の --from・--to にそのまま渡せる形で書く
func at(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64) + "s"
}

func dur(d time.Duration) string {
	return domain.FormatDuration(d)
}

func frame(f domain.Frame) string {
	if f.Function == "" {
		return ""
	}
	if loc := f.Location(); loc != "" {
		return f.Function + " (" + loc + ")"
	}
	return f.Function
}

// paths は cli に返す GUI の URL を組む
type paths struct {
	base string
}

func (p paths) issue(id domain.IssueID, occurrence int) string {
	u := p.base + "/issues/" + url.PathEscape(string(id))
	if occurrence > 0 {
		u += "?occurrence=" + strconv.Itoa(occurrence)
	}
	return u
}

func (p paths) goroutine(id domain.GoroutineID, from, to time.Duration) string {
	return fmt.Sprintf("%s/goroutines/%d?from=%s&to=%s", p.base, id, at(from), at(to))
}

func (p paths) scheduler(from, to time.Duration) string {
	return fmt.Sprintf("%s/scheduler?from=%s&to=%s", p.base, at(from), at(to))
}

func goroutineCommand(id domain.GoroutineID, from, to time.Duration) string {
	return fmt.Sprintf("gorviz goroutine %d --from %s --to %s", id, at(from), at(to))
}

func parseAt(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, domain.ErrInvalidArgument.With(fmt.Sprintf("%q is not a time since the start of the session; write it like 12.5s", s))
	}
	return d, nil
}
