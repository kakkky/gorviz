package api

import (
	"context"
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/protocol/server"
)

type startResponse struct {
	Started bool        `json:"started"`
	App     appResponse `json:"app"`
	GUI     string      `json:"gui,omitempty"`
	Log     string      `json:"serverLog"`
	Next    []string    `json:"next"`
}

// GetStartResultHandler はアプリが動き出す (ビルドに失敗する) まで待って返す。失敗したら、返したあとに server を止める
type GetStartResultHandler struct {
	wait    *usecase.WaitAppStartUsecase
	stop    context.CancelFunc
	guiBase string
}

func NewGetStartResultHandler(wait *usecase.WaitAppStartUsecase, stop context.CancelFunc, guiBase string) *GetStartResultHandler {
	return &GetStartResultHandler{wait: wait, stop: stop, guiBase: guiBase}
}

func (h *GetStartResultHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	out, err := h.wait.Exec(r.Context(), usecase.WaitAppStartUsecaseInput{})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	resp := startResponse{Started: out.Started, App: appOf(out.App), Log: server.LogPath()}
	code := 0
	if out.Started {
		resp.GUI = h.guiBase
		resp.Next = []string{"gorviz docs getting-started", "gorviz diagnose <scenario.yaml>", "gorviz status"}
	} else {
		code = 1
		resp.Next = []string{"fix the error and run gorviz run again"}
	}
	if err := write(rw, r, resp, code); err != nil {
		middleware.Fail(r, err)
		return
	}
	if !out.Started {
		h.stop()
	}
}
