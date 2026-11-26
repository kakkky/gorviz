package function

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/usecase"
)

// FunctionCallHandler は関数呼び出しで届くもの (エントリの init からの Start と、アプリの main の defer からの Stop) を受ける。どちらも終わるまで戻らない
type FunctionCallHandler struct {
	start  *usecase.StartAppObservationUsecase
	finish *usecase.FinishAppObservationUsecase
}

func NewFunctionCallHandler(uc *usecase.UseCase) *FunctionCallHandler {
	return &FunctionCallHandler{start: uc.NewStartAppObservationUsecase(), finish: uc.NewFinishAppObservationUsecase()}
}

func (h *FunctionCallHandler) Start() error {
	_, err := h.start.Exec(context.Background(), usecase.StartAppObservationUsecaseInput{})
	return err
}

// Stop は panic を握りつぶさない。main の goroutine の panic は、trace を書き切ったあとにそのまま続ける
func (h *FunctionCallHandler) Stop() {
	_, _ = h.finish.Exec(context.Background(), usecase.FinishAppObservationUsecaseInput{})
}
