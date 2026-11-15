package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type GetSnapshotStreamHandler struct {
	getRuntimeSnapshotUsecase *usecase.GetRuntimeSnapshotUsecase
	listIssuesUsecase         *usecase.ListIssuesUsecase
	interval                  time.Duration
	buckets                   int
}

func NewGetSnapshotStreamHandler(getRuntimeSnapshotUsecase *usecase.GetRuntimeSnapshotUsecase, listIssuesUsecase *usecase.ListIssuesUsecase, interval time.Duration, buckets int) *GetSnapshotStreamHandler {
	return &GetSnapshotStreamHandler{
		getRuntimeSnapshotUsecase: getRuntimeSnapshotUsecase,
		listIssuesUsecase:         listIssuesUsecase,
		interval:                  interval,
		buckets:                   buckets,
	}
}

func (h *GetSnapshotStreamHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// 画面を開いている間ずっと書き続けるので、http.Server の WriteTimeout をこのリクエストだけ外す
	if err := http.NewResponseController(rw).SetWriteDeadline(time.Time{}); err != nil {
		slog.ErrorContext(ctx, "clear write deadline for snapshot stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	sse, err := turbo.NewStreamSSEWriter(rw)
	if err != nil {
		slog.ErrorContext(ctx, "open snapshot stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		out, err := h.getRuntimeSnapshotUsecase.Exec(ctx, usecase.GetRuntimeSnapshotUsecaseInput{})
		if ctx.Err() != nil {
			return
		}
		// まだ録画ループが 1 度も観測できていなければ、次の周期まで待つ。対象に繋がらない理由はサーバのログに出ている
		if err != nil {
			if !errors.Is(err, model.ErrNotFound) {
				slog.WarnContext(ctx, "get runtime snapshot", slog.Any("err", err))
			}
		} else {
			replayJSON, err := newGMPReplayJSON(out.Snapshot.Timeline, out.Snapshot.LeakedGoroutines, out.Snapshot.TimelineStartedAt).marshal()
			if err != nil {
				slog.ErrorContext(ctx, "encode gmp replay", slog.Any("err", err))
				return
			}
			vm := toSnapshotViewModel(out.Snapshot, h.buckets)
			if issues, err := h.listIssuesUsecase.Exec(ctx, usecase.ListIssuesUsecaseInput{Now: time.Now()}); err == nil {
				vm.IssueCount, vm.ErrorCount, vm.SlowCount = len(issues.Issues), issues.ErrorsLastMinute, issues.SlowLastMinute
			}
			if err := sse.Write(ctx, partials.SnapshotStreamUpdate(vm, replayJSON)); err != nil {
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func toSnapshotViewModel(out usecase.RuntimeSnapshot, buckets int) components.SnapshotViewModel {
	tl := out.Timeline
	procs := domain.SortedProcs(tl)

	vm := components.SnapshotViewModel{
		At:             out.At.Format(time.TimeOnly),
		Window:         tl.Window.Round(time.Millisecond).String(),
		ProcCount:      len(procs),
		ThreadCount:    len(tl.Threads),
		GoroutineCount: out.GoroutineCount,
		Created:        tl.GoroutinesCreated,
		Exited:         tl.GoroutinesDestroyed,
		History:        toSparklineViewModel(out.GoroutineHistory),
		LeakCheck:      string(out.LeakCheckStatus),
		DumpAt:         out.DumpAt.Format(time.TimeOnly),
		HeapLive:       formatBytes(out.Runtime.HeapLiveBytes),
		HeapGoal:       formatBytes(out.Runtime.HeapGoalBytes),
		GCPerSecond:    fmt.Sprintf("%.1f", out.Runtime.GCPerSecond),
		CPUWaiting:     fmt.Sprintf("%.2f", out.Runtime.CPUWaiting),
		Recording:      "-",
	}
	if out.HasRecording {
		r := out.Recording
		vm.Recording = fmt.Sprintf("%s–%s (%s), %s / %s", r.From.Format(time.TimeOnly), r.To.Format(time.TimeOnly), r.To.Sub(r.From).Round(time.Second), formatBytes(uint64(r.Bytes)), formatBytes(uint64(r.LimitBytes)))
	}

	for _, s := range []struct {
		label string
		cat   model.GoroutineStateCategory
	}{
		{"running", model.GoroutineRunning},
		{"runnable", model.GoroutineRunnable},
		{"syscall", model.GoroutineSyscall},
		{"waiting", model.GoroutineWaiting},
	} {
		count := out.StateCounts[s.cat]
		percent := 0.0
		if out.GoroutineCount > 0 {
			percent = float64(count) / float64(out.GoroutineCount) * 100
		}
		vm.States = append(vm.States, components.GoroutineStateViewModel{
			Label:   s.label,
			Count:   count,
			Percent: fmt.Sprintf("%.2f%%", percent),
			Class:   "state-" + s.label,
		})
	}

	for _, lane := range procs {
		laneVM := components.ProcLaneViewModel{
			Proc:        fmt.Sprintf("P%d", lane.Proc),
			Utilization: fmt.Sprintf("%.0f%%", domain.Utilization(lane, tl.Window)*100),
		}
		for _, b := range domain.Buckets(lane, tl.Window, buckets) {
			if b.Busy == 0 {
				laneVM.Cells = append(laneVM.Cells, components.LaneCellViewModel{Title: "idle"})
				continue
			}
			laneVM.Cells = append(laneVM.Cells, components.LaneCellViewModel{
				Style: "background:" + goroutineColor(b.Goroutine, b.Busy),
				Title: fmt.Sprintf("G%d on M%d (busy %.0f%%)", b.Goroutine, b.Thread, b.Busy*100),
			})
		}
		vm.Lanes = append(vm.Lanes, laneVM)
	}

	for _, s := range out.Scopes {
		vm.Scopes = append(vm.Scopes, components.ScopeViewModel{Name: s.Name, Goroutines: s.Goroutines, Waiting: s.Waiting, Leaked: s.Leaked})
	}
	for _, o := range out.TopOrigins {
		vm.TopOrigins = append(vm.TopOrigins, components.OriginCountViewModel{Count: o.Count, Origin: string(o.Origin)})
	}
	return vm
}

func formatBytes(b uint64) string {
	const mib = 1 << 20
	if b == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f MiB", float64(b)/mib)
}

// goroutineColor は G の ID から色相を決める。黄金角ずつずらして、ID が近い G 同士でも色が離れるようにする
func goroutineColor(id model.GoroutineID, busy float64) string {
	hue := (int64(id) * 137) % 360
	lightness := 25 + 35*min(busy, 1)
	return fmt.Sprintf("hsl(%d 70%% %.0f%%)", hue, lightness)
}

func toSparklineViewModel(values []int) components.SparklineViewModel {
	vm := components.SparklineViewModel{Samples: len(values)}
	if len(values) == 0 {
		return vm
	}
	vm.Min, vm.Max = values[0], values[0]
	for _, v := range values {
		vm.Min, vm.Max = min(vm.Min, v), max(vm.Max, v)
	}
	points := make([]string, 0, len(values))
	for i, v := range values {
		x := 100.0
		if len(values) > 1 {
			x = float64(i) / float64(len(values)-1) * 100
		}
		y := 50.0
		if vm.Max > vm.Min {
			y = 95 - float64(v-vm.Min)/float64(vm.Max-vm.Min)*90
		}
		points = append(points, fmt.Sprintf("%.1f,%.1f", x, y))
	}
	vm.Points = strings.Join(points, " ")
	return vm
}
