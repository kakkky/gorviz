package terminal

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/usecase"
)

const (
	clearScreen = "\x1b[H\x1b[2J"
	reset       = "\x1b[0m"
	dim         = "\x1b[2m"
	bold        = "\x1b[1m"
	red         = "\x1b[31m"
	green       = "\x1b[32m"
	yellow      = "\x1b[33m"
	maxLineLen  = 120
	// 待機中の G のまとまりは長いプロセスだと数十になるので、多い順に上だけ出す
	maxWaitingGroups = 8
)

// goroutineColors は 256 色の中から、隣り合っても見分けやすい色を選んだもの
var goroutineColors = []int{39, 208, 41, 171, 226, 45, 202, 118, 213, 33, 220, 87}

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

type Top struct {
	getRuntimeSnapshotUsecase *usecase.GetRuntimeSnapshotUsecase
	getSchedTraceUsecase      *usecase.GetSchedTraceUsecase
	targetURL                 string
	interval                  time.Duration
	buckets                   int
	out                       io.Writer
}

func NewTop(cfg *config.Config, uc *usecase.UseCase) *Top {
	return &Top{
		getRuntimeSnapshotUsecase: uc.NewGetRuntimeSnapshotUsecase(),
		getSchedTraceUsecase:      uc.NewGetSchedTraceUsecase(),
		targetURL:                 cfg.TargetURL,
		interval:                  cfg.Interval,
		buckets:                   min(cfg.TimelineBuckets, 80),
		out:                       os.Stdout,
	}
}

func (t *Top) Run(ctx context.Context) error {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		out, err := t.getRuntimeSnapshotUsecase.Exec(ctx, usecase.GetRuntimeSnapshotUsecaseInput{})
		if ctx.Err() != nil {
			return nil
		}
		var sb strings.Builder
		sb.WriteString(clearScreen)
		if err != nil {
			fmt.Fprintf(&sb, "%sgorviz%s — %s\n\n%swaiting for the first snapshot of the target...%s\n", bold, reset, t.targetURL, dim, reset)
		} else {
			t.render(&sb, out.Snapshot)
		}
		if st, _ := t.getSchedTraceUsecase.Exec(ctx, usecase.GetSchedTraceUsecaseInput{}); st.Available {
			renderSchedTrace(&sb, st.Report)
		}
		if _, err := io.WriteString(t.out, sb.String()); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (t *Top) render(sb *strings.Builder, out usecase.RuntimeSnapshot) {
	tl := out.Timeline
	procs := tl.SortedProcs()

	fmt.Fprintf(sb, "%sgorviz%s — %s  %s%s  (Ctrl-C to quit)%s\n\n", bold, reset, t.targetURL, dim, out.At.Format(time.TimeOnly), reset)

	fmt.Fprintf(sb, "%sGMP%s  P=%d  M=%d (seen in trace)  G=%d   trace window=%s  created=+%d exited=-%d\n",
		bold, reset, len(procs), len(tl.Threads), out.GoroutineCount, tl.Window.Round(time.Millisecond), tl.GoroutinesCreated, tl.GoroutinesDestroyed)
	fmt.Fprintf(sb, "  G states: running %d  runnable %d  syscall %d  waiting %d\n\n",
		out.StateCounts[domain.GoroutineRunning], out.StateCounts[domain.GoroutineRunnable], out.StateCounts[domain.GoroutineSyscall], out.StateCounts[domain.GoroutineWaiting])

	renderGMP(sb, out)

	colWidth := time.Duration(0)
	if t.buckets > 0 {
		colWidth = tl.Window / time.Duration(t.buckets)
	}
	fmt.Fprintf(sb, "%sP timeline%s %s(1 col = %s, color = goroutine, · = idle)%s\n", bold, reset, dim, colWidth.Round(time.Microsecond), reset)
	for _, lane := range procs {
		fmt.Fprintf(sb, "  P%-3d ", lane.Proc)
		for _, b := range lane.Buckets(tl.Window, t.buckets) {
			sb.WriteString(bucketCell(b))
		}
		fmt.Fprintf(sb, " %3.0f%%", lane.Utilization(tl.Window)*100)
		if s, ok := lane.LastSpan(); ok {
			fmt.Fprintf(sb, "  %slast: G%d on M%d%s", dim, s.Goroutine, s.Thread, reset)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	fmt.Fprintf(sb, "%sGoroutines%s (last %d polls)  %s  %d\n\n", bold, reset, len(out.GoroutineHistory), sparkline(out.GoroutineHistory), out.GoroutineCount)

	fmt.Fprintf(sb, "%sTop origins%s\n", bold, reset)
	for _, o := range out.TopOrigins {
		fmt.Fprintf(sb, "  %6d  %s\n", o.Count, truncate(string(o.Origin), maxLineLen-10))
	}
	sb.WriteString("\n")

	fmt.Fprintf(sb, "%sLeak suspects%s %s(runtime leak check: %s)%s\n", bold, reset, dim, out.LeakCheckStatus, reset)
	if len(out.LeakSuspects) == 0 {
		fmt.Fprintf(sb, "  %snone%s\n", dim, reset)
	}
	for _, s := range out.LeakSuspects {
		label := yellow + "SUSPECT  " + reset
		if s.Confirmed {
			label = red + "LEAKED   " + reset
		}
		fmt.Fprintf(sb, "  %s%6d  %+5d  wait %-5s  %s\n", label, s.Count, s.Growth, formatWait(s.LongestWait), truncate(string(s.Origin), maxLineLen-40))
	}
}

// renderGMP は観測窓の終わりの GMP を、GUI の概念図と同じ構造 (GRQ / P ごとの M・G・LRQ / 待機 / 終了) で文字にする
func renderGMP(sb *strings.Builder, out usecase.RuntimeSnapshot) {
	st := out.Timeline.StateAt(out.Timeline.Window)
	entries := out.Timeline.Entries
	leaked := map[domain.GoroutineID]bool{}
	for _, g := range out.LeakedGoroutines {
		leaked[g] = true
	}

	fmt.Fprintf(sb, "%sGMP at end of trace window%s %s(LRQ / GRQ are inferred from the trace)%s\n", bold, reset, dim, reset)
	fmt.Fprintf(sb, "  GRQ  %s\n", goroutineList(st.GlobalQueue, 12))
	for _, p := range st.Procs {
		status := fmt.Sprintf("%-8s", p.Status)
		switch p.Status {
		case domain.ProcRunning:
			status = green + status + reset
		case domain.ProcSyscall:
			status = red + status + reset
		default:
			status = dim + status + reset
		}
		onCPU := "-"
		if p.HasThread {
			onCPU = fmt.Sprintf("M%d", p.Thread)
		}
		// 色の制御文字が幅に数えられないよう、桁をそろえてから色を付ける
		fmt.Fprintf(sb, "  P%-3d %s %-5s LRQ %-28s", p.Proc, status, onCPU, goroutineList(p.LocalQueue, 4))
		if p.HasRunning {
			fmt.Fprintf(sb, " ▶ G%-6d %s", p.Running, entryLabel(entries, p.Running))
		}
		sb.WriteString("\n")
	}
	for _, h := range st.HandedOff {
		fmt.Fprintf(sb, "  %s(P handed off)%s M%d ▶ G%d in syscall  %s\n", dim, reset, h.Thread, h.Goroutine, entryLabel(entries, h.Goroutine))
	}

	groups := slices.Collect(maps.Keys(st.Waiting))
	slices.SortFunc(groups, func(a, b domain.WaitingGroup) int { return len(st.Waiting[b]) - len(st.Waiting[a]) })
	fmt.Fprintf(sb, "  waiting  %s(entry function — wait reason)%s\n", dim, reset)
	for _, key := range groups[:min(len(groups), maxWaitingGroups)] {
		n := 0
		for _, g := range st.Waiting[key] {
			if leaked[g] {
				n++
			}
		}
		fmt.Fprintf(sb, "    %5d  %s — %s", len(st.Waiting[key]), shortEntry(key.Entry), key.Reason)
		if n > 0 {
			fmt.Fprintf(sb, " %s(%d leaked)%s", red, n, reset)
		}
		sb.WriteString("\n")
	}
	if len(groups) > maxWaitingGroups {
		fmt.Fprintf(sb, "    %s+%d more groups%s\n", dim, len(groups)-maxWaitingGroups, reset)
	}
	fmt.Fprintf(sb, "  gFree    exited in window: %d\n\n", len(st.Dead))
}

func renderSchedTrace(sb *strings.Builder, r domain.SchedTraceReport) {
	fmt.Fprintf(sb, "\n%sruntime-reported (schedtrace)%s %suptime %s%s\n", bold, reset, dim, r.Uptime, reset)
	fmt.Fprintf(sb, "  GRQ length %d   P idle %d/%d   M %d (idle %d, spinning %d)\n", r.GlobalRunQueue, r.IdleProcs, r.GOMAXPROCS, r.Threads, r.IdleThreads, r.SpinningThreads)
	for _, p := range r.Procs {
		m := "-"
		if p.HasThread {
			m = fmt.Sprintf("M%d", p.Thread)
		}
		fmt.Fprintf(sb, "  P%-3d %-8s %-5s LRQ length %-4d gFree %d\n", p.Proc, p.Status, m, p.RunQueue, p.GFree)
	}
}

func entryLabel(entries map[domain.GoroutineID]string, g domain.GoroutineID) string {
	return shortEntry(entries[g])
}

func shortEntry(fn string) string {
	if fn == "" {
		return "?"
	}
	return domain.ShortFunctionName(fn)
}

func goroutineList(gs []domain.GoroutineID, max int) string {
	if len(gs) == 0 {
		return "[]"
	}
	items := make([]string, 0, min(len(gs), max))
	for _, g := range gs[:min(len(gs), max)] {
		items = append(items, fmt.Sprintf("G%d", g))
	}
	if len(gs) > max {
		items = append(items, fmt.Sprintf("+%d", len(gs)-max))
	}
	return "[" + strings.Join(items, " ") + "]"
}

func bucketCell(b domain.LaneBucket) string {
	var r string
	switch {
	case b.Busy == 0:
		return dim + "·" + reset
	case b.Busy >= 0.66:
		r = "█"
	case b.Busy >= 0.33:
		r = "▓"
	default:
		r = "░"
	}
	color := goroutineColors[int(b.Goroutine)%len(goroutineColors)]
	return fmt.Sprintf("\x1b[38;5;%dm%s%s", color, r, reset)
}

func sparkline(values []int) string {
	if len(values) == 0 {
		return ""
	}
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo, hi = min(lo, v), max(hi, v)
	}
	var sb strings.Builder
	for _, v := range values {
		i := 0
		if hi > lo {
			i = (v - lo) * (len(sparkRunes) - 1) / (hi - lo)
		}
		sb.WriteRune(sparkRunes[i])
	}
	return sb.String()
}

// goroutine dump の待機時間は分単位でしか出ないので、1 分未満は区別できない
func formatWait(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	return fmt.Sprintf("≥%dm", int(d.Minutes()))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}
