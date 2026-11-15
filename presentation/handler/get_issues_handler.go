package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/presentation/view/pages"
	"github.com/kakkky/gorviz/usecase"
)

type GetIssuesHandler struct {
	listIssuesUsecase *usecase.ListIssuesUsecase
	recordings        func() []domain.SavedRecording
	targetURL         string
}

func NewGetIssuesHandler(listIssuesUsecase *usecase.ListIssuesUsecase, recordings func() []domain.SavedRecording, targetURL string) *GetIssuesHandler {
	return &GetIssuesHandler{listIssuesUsecase: listIssuesUsecase, recordings: recordings, targetURL: targetURL}
}

func (h *GetIssuesHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	ctx := r.Context()
	now := time.Now()
	out, err := h.listIssuesUsecase.Exec(ctx, usecase.ListIssuesUsecaseInput{Now: now})
	if err != nil {
		slog.ErrorContext(ctx, "list issues", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	vm := pages.IssuesViewModel{TargetURL: h.targetURL, At: now.Format(time.TimeOnly)}
	for _, i := range out.Issues {
		vm.Issues = append(vm.Issues, toIssueViewModel(i))
	}
	for _, rec := range h.recordings() {
		rvm := pages.RecordingViewModel{Kind: string(rec.Kind), Subject: rec.Subject, At: rec.At.Format(time.TimeOnly), Span: fmt.Sprintf("%s–%s", rec.From.Format(time.TimeOnly), rec.To.Format(time.TimeOnly)), Size: formatBytes(uint64(rec.Bytes))}
		for n := range rec.Chunks {
			rvm.Downloads = append(rvm.Downloads, pages.IssueLinkViewModel{Label: fmt.Sprintf("chunk %d", n), URL: fmt.Sprintf("/api/recordings/%s/chunks/%d", url.PathEscape(rec.ID), n)})
		}
		vm.Recordings = append(vm.Recordings, rvm)
	}
	if err := pages.Issues(vm).Render(ctx, rw); err != nil {
		slog.ErrorContext(ctx, "render issues", slog.Any("err", err))
	}
}

// toIssueViewModel は種類ごとの調べに入る先を並べる。代表の G があれば Goroutine、ログが絡む種類は絞った Logs
func toIssueViewModel(i domain.Issue) pages.IssueViewModel {
	vm := pages.IssueViewModel{
		Kind:      string(i.Kind),
		Certainty: string(i.Certainty),
		Subject:   i.Subject,
		Summary:   i.Summary,
		Location:  i.Location,
	}
	if len(i.Trend) > 0 {
		vm.Trend = toSparklineViewModel(i.Trend)
	}
	if i.HasGoroutine {
		vm.Links = append(vm.Links, pages.IssueLinkViewModel{Label: fmt.Sprintf("G%d", i.Goroutine), URL: goroutineURL(i.Goroutine, i.At, i.From)})
	}
	if i.LogQuery != "" {
		vm.Links = append(vm.Links, pages.IssueLinkViewModel{Label: "logs", URL: "/logs?" + url.Values{"q": {i.LogQuery}}.Encode()})
	}
	if i.Kind == domain.IssueCPUWaitSpike {
		vm.Links = append(vm.Links, pages.IssueLinkViewModel{Label: "scheduler", URL: "/scheduler"})
	}
	return vm
}
