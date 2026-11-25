package web

import (
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/assets"
)

// NewAssetsHandler は esbuild で書き出した JS・CSS を配る。URL に版を付けているので、長くキャッシュさせる
func NewAssetsHandler() http.Handler {
	files := http.StripPrefix(assets.URLPrefix, http.FileServerFS(assets.FS()))
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(rw, r)
	})
}
