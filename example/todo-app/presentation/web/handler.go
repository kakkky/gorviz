// Package web は Todo の画面とフォームの受け口
package web

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
	"github.com/kakkky/gorviz/example/todo-app/presentation/middleware"
	"github.com/kakkky/gorviz/example/todo-app/usecase"
)

//go:embed view/index.html
var views embed.FS

var indexPage = template.Must(template.ParseFS(views, "view/index.html"))

const seedCount = 1000

func redirect(w http.ResponseWriter, r *http.Request, key, msg string) {
	http.Redirect(w, r, "/?"+key+"="+url.QueryEscape(msg), http.StatusSeeOther)
}

func todoID(r *http.Request) (domain.TodoID, error) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return 0, domain.ErrInvalidArgument.With("todo id is not a number")
	}
	return domain.TodoID(id), nil
}

type GetIndexHandler struct {
	uc *usecase.UseCase
}

func NewGetIndexHandler(uc *usecase.UseCase) *GetIndexHandler {
	return &GetIndexHandler{uc: uc}
}

type indexViewModel struct {
	Todos   []domain.Todo
	Query   string
	Total   int
	Flash   string
	Error   string
	Stats   *statsViewModel
	Backup  string
	Elapsed string
}

type statsViewModel struct {
	Total, Done, Duplicates int
	Took                    string
}

func (h *GetIndexHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	q := r.URL.Query()
	list, err := h.uc.NewListTodosUsecase().Exec(r.Context(), usecase.ListTodosUsecaseInput{Query: q.Get("q")})
	if err != nil {
		middleware.SetError(r.Context(), err)
		return
	}
	vm := indexViewModel{Todos: list.Todos, Query: q.Get("q"), Total: list.Total, Flash: q.Get("flash"), Error: q.Get("error")}
	if q.Get("stats") == "1" {
		stats, err := h.uc.NewComputeTodoStatsUsecase().Exec(r.Context(), usecase.ComputeTodoStatsUsecaseInput{})
		if err != nil {
			middleware.SetError(r.Context(), err)
			return
		}
		vm.Stats = &statsViewModel{Total: stats.Stats.Total, Done: stats.Stats.Done, Duplicates: stats.Stats.Duplicates, Took: stats.Took.Round(time.Millisecond).String()}
	}
	if last, _ := h.uc.NewGetLastBackupUsecase().Exec(r.Context(), usecase.GetLastBackupUsecaseInput{}); last.Has {
		vm.Backup = last.At.Format(time.TimeOnly)
	}
	vm.Elapsed = time.Since(start).Round(time.Millisecond).String()
	if err := indexPage.Execute(w, vm); err != nil {
		slog.ErrorContext(r.Context(), "render index", slog.Any("err", err))
	}
}

type PostTodosHandler struct {
	uc *usecase.UseCase
}

func NewPostTodosHandler(uc *usecase.UseCase) *PostTodosHandler {
	return &PostTodosHandler{uc: uc}
}

func (h *PostTodosHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	out, err := h.uc.NewAddTodoUsecase().Exec(r.Context(), usecase.AddTodoUsecaseInput{Title: r.FormValue("title"), Reminder: r.FormValue("reminder") == "on"})
	if err != nil {
		middleware.SetError(r.Context(), err)
		return
	}
	if out.Todo.Reminder && !out.ReminderRegistered {
		redirect(w, r, "error", "todo added, but the reminder could not be registered (timeout)")
		return
	}
	redirect(w, r, "flash", fmt.Sprintf("added #%d", out.Todo.ID))
}

type PostCompleteTodoHandler struct {
	uc *usecase.UseCase
}

func NewPostCompleteTodoHandler(uc *usecase.UseCase) *PostCompleteTodoHandler {
	return &PostCompleteTodoHandler{uc: uc}
}

func (h *PostCompleteTodoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := todoID(r)
	if err != nil {
		middleware.SetError(r.Context(), err)
		return
	}
	out, err := h.uc.NewCompleteTodoUsecase().Exec(r.Context(), usecase.CompleteTodoUsecaseInput{ID: id})
	if err != nil {
		middleware.SetError(r.Context(), err)
		return
	}
	redirect(w, r, "flash", fmt.Sprintf("completed #%d", out.Todo.ID))
}

type PostDeleteTodoHandler struct {
	uc *usecase.UseCase
}

func NewPostDeleteTodoHandler(uc *usecase.UseCase) *PostDeleteTodoHandler {
	return &PostDeleteTodoHandler{uc: uc}
}

func (h *PostDeleteTodoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := todoID(r)
	if err != nil {
		middleware.SetError(r.Context(), err)
		return
	}
	if _, err := h.uc.NewDeleteTodoUsecase().Exec(r.Context(), usecase.DeleteTodoUsecaseInput{ID: id}); err != nil {
		middleware.SetError(r.Context(), err)
		return
	}
	redirect(w, r, "flash", fmt.Sprintf("deleted #%d", id))
}

type PostSeedHandler struct {
	uc *usecase.UseCase
}

func NewPostSeedHandler(uc *usecase.UseCase) *PostSeedHandler {
	return &PostSeedHandler{uc: uc}
}

func (h *PostSeedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := h.uc.NewSeedTodosUsecase().Exec(r.Context(), usecase.SeedTodosUsecaseInput{N: seedCount}); err != nil {
		middleware.SetError(r.Context(), err)
		return
	}
	redirect(w, r, "flash", fmt.Sprintf("added %d sample todos", seedCount))
}

type PostBackupHandler struct {
	uc *usecase.UseCase
}

func NewPostBackupHandler(uc *usecase.UseCase) *PostBackupHandler {
	return &PostBackupHandler{uc: uc}
}

func (h *PostBackupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	out, err := h.uc.NewBackupTodosUsecase().Exec(r.Context(), usecase.BackupTodosUsecaseInput{})
	if err != nil {
		middleware.SetError(r.Context(), err)
		return
	}
	redirect(w, r, "flash", "backed up to "+out.Path)
}

type GetExportCSVHandler struct {
	uc *usecase.UseCase
}

func NewGetExportCSVHandler(uc *usecase.UseCase) *GetExportCSVHandler {
	return &GetExportCSVHandler{uc: uc}
}

func (h *GetExportCSVHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	out, err := h.uc.NewExportTodosCSVUsecase().Exec(r.Context(), usecase.ExportTodosCSVUsecaseInput{})
	if err != nil {
		middleware.SetError(r.Context(), err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="todos.csv"`)
	_, _ = w.Write([]byte(out.CSV))
}
