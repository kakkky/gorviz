// Package web は GUI (ブラウザ) の画面の handler。usecase の出力から画面の ViewModel を組み、templ で描く
package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/assets"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// GetRootHandler は最初のサービスの Overview へ移す。サービスがまだなければ、agent を待っている画面を出す
type GetRootHandler struct {
	services *usecase.ListServicesUsecase
}

func NewGetRootHandler(services *usecase.ListServicesUsecase) *GetRootHandler {
	return &GetRootHandler{services: services}
}

func (h *GetRootHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	out, err := h.services.Exec(r.Context(), usecase.ListServicesUsecaseInput{Now: time.Now()})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	if len(out.Services) > 0 {
		http.Redirect(rw, r, paths.Overview(out.Services[0].Name), http.StatusSeeOther)
		return
	}
	render(rw, r, pages.NoService())
}

// GetAssetsHandler は assets の dist/ を配る。URL に版 (?v=) が付いていれば中身は変わらないので、長くキャッシュさせる
type GetAssetsHandler struct {
	files http.Handler
}

func NewGetAssetsHandler() *GetAssetsHandler {
	return &GetAssetsHandler{files: http.StripPrefix(strings.TrimSuffix(assets.URLPrefix, "/"), http.FileServerFS(assets.FS()))}
}

func (h *GetAssetsHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("v") != "" {
		rw.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	h.files.ServeHTTP(rw, r)
}
