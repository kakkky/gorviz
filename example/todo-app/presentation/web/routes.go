package web

import (
	"net/http"

	"github.com/kakkky/gorviz/example/todo-app/presentation/middleware"
	"github.com/kakkky/gorviz/example/todo-app/usecase"
)

func NewRoutes(uc *usecase.UseCase) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /{$}", NewGetIndexHandler(uc))
	mux.Handle("POST /todos", NewPostTodosHandler(uc))
	mux.Handle("POST /todos/{id}/complete", NewPostCompleteTodoHandler(uc))
	mux.Handle("POST /todos/{id}/delete", NewPostDeleteTodoHandler(uc))
	mux.Handle("POST /seed", NewPostSeedHandler(uc))
	mux.Handle("POST /backup", NewPostBackupHandler(uc))
	mux.Handle("GET /export.csv", NewGetExportCSVHandler(uc))
	return middleware.Wrap(mux)
}
