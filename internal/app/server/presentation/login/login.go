// Package login はブラウザ (GUI) のログイン。トークンを入れてログインし、cookie に持たせる
package login

import (
	"net/http"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
)

const sessionAge = 30 * 24 * time.Hour

// GetLoginHandler はログインの画面
type GetLoginHandler struct{}

func NewGetLoginHandler() *GetLoginHandler { return &GetLoginHandler{} }

func writePage(rw http.ResponseWriter, r *http.Request, status int, vm pages.LoginViewModel) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(status)
	if err := pages.Login(vm).Render(r.Context(), rw); err != nil {
		middleware.SetError(r, domain.ErrInternal.Wrap(err, "render the login page"))
	}
}

func (h *GetLoginHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	writePage(rw, r, http.StatusOK, pages.LoginViewModel{Next: r.URL.Query().Get("next")})
}

// PostLoginHandler はトークンを確かめて cookie に持たせる。トークンを無効にすると、その cookie も使えなくなる
type PostLoginHandler struct {
	tokens middleware.TokenChecker
	secure bool
}

func NewPostLoginHandler(cfg *config.Config, tokens middleware.TokenChecker) *PostLoginHandler {
	return &PostLoginHandler{tokens: tokens, secure: !cfg.Insecure}
}

func (h *PostLoginHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		middleware.SetError(r, domain.ErrInvalidArgument.With("the login form is broken"))
		return
	}
	token, next := strings.TrimSpace(r.PostForm.Get("token")), r.PostForm.Get("next")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	if !h.tokens.ValidAPIToken(token) {
		writePage(rw, r, http.StatusUnauthorized, pages.LoginViewModel{Next: next, Message: "the token is not valid"})
		return
	}
	http.SetCookie(rw, &http.Cookie{Name: middleware.SessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionAge.Seconds())})
	http.Redirect(rw, r, next, http.StatusSeeOther)
}

// PostLogoutHandler は cookie を消す
type PostLogoutHandler struct{}

func NewPostLogoutHandler() *PostLogoutHandler { return &PostLogoutHandler{} }

func (h *PostLogoutHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	http.SetCookie(rw, &http.Cookie{Name: middleware.SessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(rw, r, "/login", http.StatusSeeOther)
}
