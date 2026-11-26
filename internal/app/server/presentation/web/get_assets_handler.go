package web

import (
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/assets"
)

// GetAssetsHandler は esbuild で書き出した JS・CSS を配る。URL に版を付けているので、長くキャッシュさせる
type GetAssetsHandler struct {
	files http.Handler
}

func NewGetAssetsHandler() *GetAssetsHandler {
	return &GetAssetsHandler{files: http.StripPrefix(assets.URLPrefix, http.FileServerFS(assets.FS()))}
}

func (h *GetAssetsHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	h.files.ServeHTTP(rw, r)
}
