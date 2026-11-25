package web

import (
	"net/http"

	"github.com/a-h/templ"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
)

func render(rw http.ResponseWriter, r *http.Request, c templ.Component) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), rw); err != nil {
		middleware.Fail(r, domain.ErrInternal.Wrap(err, "render a page"))
	}
}
