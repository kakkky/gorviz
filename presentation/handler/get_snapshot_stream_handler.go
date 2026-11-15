package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/presentation/view/components"
	"github.com/kakkky/gorviz/presentation/view/partials"
	"github.com/kakkky/gorviz/usecase"
)

type GetSnapshotStreamHandler struct {
	getRuntimeSnapshotUsecase *usecase.GetRuntimeSnapshotUsecase
	getSchedTraceUsecase      *usecase.GetSchedTraceUsecase
	interval                  time.Duration
	buckets                   int
}

func NewGetSnapshotStreamHandler(getRuntimeSnapshotUsecase *usecase.GetRuntimeSnapshotUsecase, getSchedTraceUsecase *usecase.GetSchedTraceUsecase, interval time.Duration, buckets int) *GetSnapshotStreamHandler {
	return &GetSnapshotStreamHandler{
		getRuntimeSnapshotUsecase: getRuntimeSnapshotUsecase,
		getSchedTraceUsecase:      getSchedTraceUsecase,
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
			if !errors.Is(err, domain.ErrNotFound) {
				slog.WarnContext(ctx, "get runtime snapshot", slog.Any("err", err))
			}
		} else {
			replayJSON, err := newGMPReplayJSON(out.Snapshot.Timeline, out.Snapshot.LeakedGoroutines).marshal()
			if err != nil {
				slog.ErrorContext(ctx, "encode gmp replay", slog.Any("err", err))
				return
			}
			if err := sse.Write(ctx, partials.SnapshotStreamUpdate(toSnapshotViewModel(out.Snapshot, h.buckets), replayJSON)); err != nil {
				return
			}
		}

		if st, _ := h.getSchedTraceUsecase.Exec(ctx, usecase.GetSchedTraceUsecaseInput{}); st.Available {
			if err := sse.Write(ctx, partials.SchedTraceStreamUpdate(toSchedTraceViewModel(st.Report))); err != nil {
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
	procs := tl.SortedProcs()

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
	}

	for _, s := range []struct {
		label string
		cat   domain.GoroutineStateCategory
	}{
		{"running", domain.GoroutineRunning},
		{"runnable", domain.GoroutineRunnable},
		{"syscall", domain.GoroutineSyscall},
		{"waiting", domain.GoroutineWaiting},
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
			Utilization: fmt.Sprintf("%.0f%%", lane.Utilization(tl.Window)*100),
		}
		for _, b := range lane.Buckets(tl.Window, buckets) {
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

	for _, o := range out.TopOrigins {
		vm.TopOrigins = append(vm.TopOrigins, components.OriginCountViewModel{Count: o.Count, Origin: string(o.Origin)})
	}
	for _, s := range out.Leaks {
		vm.Leaks = append(vm.Leaks, components.LeakViewModel{
			Certainty:   string(s.Certainty),
			Count:       s.Count,
			Growth:      fmt.Sprintf("%+d", s.Growth),
			LongestWait: formatWait(s.LongestWait),
			Origin:      string(s.Origin),
		})
	}
	return vm
}

// goroutine dump の待機時間は分単位でしか出ないので、1 分未満は区別できない
func formatWait(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	return fmt.Sprintf("≥%dm", int(d.Minutes()))
}

// goroutineColor は G の ID から色相を決める。黄金角ずつずらして、ID が近い G 同士でも色が離れるようにする
func goroutineColor(id domain.GoroutineID, busy float64) string {
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

func toSchedTraceViewModel(r domain.SchedTraceReport) components.SchedTraceViewModel {
	vm := components.SchedTraceViewModel{
		Uptime:          r.Uptime.String(),
		GOMAXPROCS:      r.GOMAXPROCS,
		IdleProcs:       r.IdleProcs,
		Threads:         r.Threads,
		SpinningThreads: r.SpinningThreads,
		IdleThreads:     r.IdleThreads,
		GlobalRunQueue:  r.GlobalRunQueue,
	}
	for _, p := range r.Procs {
		thread := "-"
		if p.HasThread {
			thread = fmt.Sprintf("M%d", p.Thread)
		}
		vm.GFreeTotal += p.GFree
		vm.Procs = append(vm.Procs, components.SchedTraceProcViewModel{
			Proc:     fmt.Sprintf("P%d", p.Proc),
			Status:   p.Status,
			Thread:   thread,
			RunQueue: p.RunQueue,
			GFree:    p.GFree,
		})
	}
	states := make([]string, 0, len(r.GoroutineStates))
	for _, name := range slices.Sorted(maps.Keys(r.GoroutineStates)) {
		states = append(states, fmt.Sprintf("%s %d", name, r.GoroutineStates[name]))
	}
	vm.GoroutineStates = strings.Join(states, " / ")
	return vm
}
