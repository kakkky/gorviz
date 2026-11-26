package web

import (
	"net/http"
)

// GetRootHandler は Overview へ移す
type GetRootHandler struct{}

func NewGetRootHandler() *GetRootHandler { return &GetRootHandler{} }

func (GetRootHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	http.Redirect(rw, r, "/overview", http.StatusSeeOther)
}
