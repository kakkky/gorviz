package main

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed index.html
var pages embed.FS

var indexPage = template.Must(template.ParseFS(pages, "index.html"))

type App struct {
	store    *Store
	notifier *Notifier
	mailer   *Mailer
	backup   *Backup
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("POST /todos", a.create)
	mux.HandleFunc("POST /todos/{id}/complete", a.complete)
	mux.HandleFunc("POST /todos/{id}/delete", a.delete)
	mux.HandleFunc("POST /seed", a.seed)
	mux.HandleFunc("POST /backup", a.runBackup)
	mux.HandleFunc("GET /export.csv", a.export)
	return accessLog(mux)
}

type indexView struct {
	Todos   []Todo
	Query   string
	Total   int
	Flash   string
	Error   string
	Stats   *statsView
	Backup  string
	Elapsed string
}

type statsView struct {
	Total, Done, Duplicates int
	Took                    string
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	q := r.URL.Query()
	todos := a.store.List(q.Get("q"))
	v := indexView{Query: q.Get("q"), Total: a.store.Count(), Flash: q.Get("flash"), Error: q.Get("error")}
	if q.Get("stats") == "1" {
		statsStart := time.Now()
		done, dups := Stats(todos)
		v.Stats = &statsView{Total: len(todos), Done: done, Duplicates: dups, Took: time.Since(statsStart).Round(time.Millisecond).String()}
	}
	if last := a.backup.LastAt(); !last.IsZero() {
		v.Backup = last.Format(time.TimeOnly)
	}
	v.Todos = todos[:min(len(todos), 200)]
	v.Elapsed = time.Since(start).Round(time.Millisecond).String()
	if err := indexPage.Execute(w, v); err != nil {
		logger.ErrorContext(r.Context(), "render index", "err", err)
	}
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		redirect(w, r, "error", "title is empty")
		return
	}
	t := a.store.Add(title, r.FormValue("reminder") == "on")
	if t.Reminder {
		if err := a.notifier.Register(r.Context(), t); err != nil {
			logger.WarnContext(r.Context(), "reminder not registered", "todo", t.ID, "err", err)
			redirect(w, r, "error", "todo added, but the reminder could not be registered (timeout)")
			return
		}
	}
	redirect(w, r, "flash", fmt.Sprintf("added #%d", t.ID))
}

func (a *App) complete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	t, ok := a.store.Complete(id)
	if !ok {
		redirect(w, r, "error", "todo not found")
		return
	}
	if err := a.mailer.SendCompleted(t); err != nil {
		logger.ErrorContext(r.Context(), "completion mail failed", "todo", t.ID, "err", err)
		redirect(w, r, "error", "completed, but the mail could not be sent")
		return
	}
	redirect(w, r, "flash", fmt.Sprintf("completed #%d", t.ID))
}

func (a *App) delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	a.store.Delete(id)
	redirect(w, r, "flash", fmt.Sprintf("deleted #%d", id))
}

func (a *App) seed(w http.ResponseWriter, r *http.Request) {
	a.store.Seed(1000)
	redirect(w, r, "flash", "added 1000 sample todos")
}

func (a *App) runBackup(w http.ResponseWriter, r *http.Request) {
	path, err := a.backup.Run()
	if err != nil {
		logger.ErrorContext(r.Context(), "backup failed", "err", err)
		redirect(w, r, "error", "backup failed")
		return
	}
	redirect(w, r, "flash", "backed up to "+path)
}

func (a *App) export(w http.ResponseWriter, r *http.Request) {
	csv := ExportCSV(a.store.List(""))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="todos.csv"`)
	_, _ = w.Write([]byte(csv))
}

func redirect(w http.ResponseWriter, r *http.Request, key, msg string) {
	http.Redirect(w, r, "/?"+key+"="+template.URLQueryEscaper(msg), http.StatusSeeOther)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// accessLog はリクエストごとに所要時間つきのログを出す。gorviz はこの duration で遅いリクエストを見つける
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.InfoContext(r.Context(), "request done", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start))
	})
}
