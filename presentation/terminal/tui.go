// Package terminal は TUI。GUI とおおむね同じ画面 (Overview・Issues・Logs・Scheduler・Goroutine) を端末で見る。
// server の API とだけ話す。入力は 1 行のコマンド (数字や "g 123" を打って Enter) で受ける
package terminal

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/adapter/serverapi"
	"github.com/kakkky/gorviz/api"
	"github.com/kakkky/gorviz/domain"
)

const (
	clearScreen = "\x1b[H\x1b[2J"
	reset       = "\x1b[0m"
	dim         = "\x1b[2m"
	bold        = "\x1b[1m"
	red         = "\x1b[31m"
	green       = "\x1b[32m"
	yellow      = "\x1b[33m"
	cyan        = "\x1b[36m"
	maxLineLen  = 120
	maxRows     = 15
)

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// goroutineColors は 256 色の中から、隣り合っても見分けやすい色を選んだもの
var goroutineColors = []int{39, 208, 41, 171, 226, 45, 202, 118, 213, 33, 220, 87}

type view string

const (
	viewOverview  view = "overview"
	viewIssues    view = "issues"
	viewLogs      view = "logs"
	viewScheduler view = "scheduler"
	viewGoroutine view = "goroutine"
)

type TUI struct {
	client   *serverapi.Client
	server   string
	interval time.Duration
	in       io.Reader
	out      io.Writer

	view     view
	logQuery string
	issues   []api.IssueJSON
	g        int64
	gAt      time.Time
	gFrom    time.Time
	message  string
}

func New(client *serverapi.Client, server string, interval time.Duration) *TUI {
	return &TUI{client: client, server: server, interval: interval, in: os.Stdin, out: os.Stdout, view: viewOverview}
}

func (t *TUI) Run(ctx context.Context) error {
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(t.in)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		if err := t.render(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case line, ok := <-lines:
			if !ok || !t.command(strings.TrimSpace(line)) {
				return nil
			}
		}
	}
}

// command は 1 行のコマンドを読む。false なら終わる
func (t *TUI) command(line string) bool {
	t.message = ""
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return true
	}
	switch fields[0] {
	case "q", "quit":
		return false
	case "1", "o":
		t.view = viewOverview
	case "2", "i":
		t.view = viewIssues
	case "3", "l":
		t.view, t.logQuery = viewLogs, strings.Join(fields[1:], " ")
	case "4", "s":
		t.view = viewScheduler
	case "g":
		// g <id>: その G を今の前後で診断する。g <n> (Issues の番号) で、その Issue の代表の G を開く
		if len(fields) < 2 {
			t.message = "usage: g <goroutine id> | g #<issue number>"
			return true
		}
		if n, ok := strings.CutPrefix(fields[1], "#"); ok {
			i, err := strconv.Atoi(n)
			if err != nil || i < 1 || i > len(t.issues) || t.issues[i-1].Goroutine == nil {
				t.message = "no goroutine for that issue"
				return true
			}
			is := t.issues[i-1]
			t.view, t.g, t.gAt, t.gFrom = viewGoroutine, *is.Goroutine, is.At, is.From
			return true
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(fields[1], "G"), 10, 64)
		if err != nil {
			t.message = "invalid goroutine id"
			return true
		}
		t.view, t.g, t.gAt, t.gFrom = viewGoroutine, id, time.Now().Add(-3*time.Second), time.Time{}
	default:
		t.message = "unknown command: " + line
	}
	return true
}

func (t *TUI) render(ctx context.Context) error {
	var sb strings.Builder
	sb.WriteString(clearScreen)
	fmt.Fprintf(&sb, "%sgorviz%s — %s  %s%s%s\n", bold, reset, t.server, dim, time.Now().Format(time.TimeOnly), reset)
	tabs := []struct {
		key string
		v   view
	}{{"1", viewOverview}, {"2", viewIssues}, {"3", viewLogs}, {"4", viewScheduler}}
	for _, tab := range tabs {
		label := fmt.Sprintf("[%s] %s", tab.key, tab.v)
		if tab.v == t.view {
			label = cyan + bold + label + reset
		}
		sb.WriteString(label + "  ")
	}
	fmt.Fprintf(&sb, "%s(g <id> | g #<issue> to diagnose a goroutine, 3 <text> to search logs, q to quit; type and Enter)%s\n\n", dim, reset)

	var err error
	switch t.view {
	case viewOverview:
		err = t.renderOverview(ctx, &sb)
	case viewIssues:
		err = t.renderIssues(ctx, &sb)
	case viewLogs:
		err = t.renderLogs(ctx, &sb)
	case viewScheduler:
		err = t.renderScheduler(ctx, &sb)
	case viewGoroutine:
		err = t.renderGoroutine(ctx, &sb)
	}
	if err != nil {
		fmt.Fprintf(&sb, "%s%s%s\n", dim, err, reset)
	}
	if t.message != "" {
		fmt.Fprintf(&sb, "\n%s%s%s\n", yellow, t.message, reset)
	}
	sb.WriteString("\n> ")
	_, werr := io.WriteString(t.out, sb.String())
	return werr
}

func (t *TUI) renderOverview(ctx context.Context, sb *strings.Builder) error {
	o, err := t.client.Overview(ctx)
	if err != nil {
		return err
	}
	issues := fmt.Sprintf("%d", o.Issues)
	if o.Issues > 0 {
		issues = red + issues + reset
	}
	fmt.Fprintf(sb, "%sOverview%s  open issues %s   goroutines %d   errors/min %d   slow requests/min %d\n", bold, reset, issues, o.Goroutines, o.ErrorsLastMinute, o.SlowLastMinute)
	fmt.Fprintf(sb, "  waiting for a P (avg) %.2f   heap %s / goal %s   GC %.1f/s\n", o.CPUWaiting, mib(o.HeapLiveBytes), mib(o.HeapGoalBytes), o.GCPerSecond)
	if o.Recording != nil {
		r := o.Recording
		fmt.Fprintf(sb, "  recorded %s–%s (%s), %s / %s\n", r.From.Format(time.TimeOnly), r.To.Format(time.TimeOnly), r.To.Sub(r.From).Round(time.Second), mib(uint64(r.Bytes)), mib(uint64(r.LimitBytes)))
	}
	fmt.Fprintf(sb, "\n%sGoroutines%s (last %d dumps)  %s  %d\n", bold, reset, len(o.GoroutineHistory), sparkline(o.GoroutineHistory), o.Goroutines)
	fmt.Fprintf(sb, "  running %d  runnable %d  syscall %d  waiting %d\n\n", o.States["running"], o.States["runnable"], o.States["syscall"], o.States["waiting"])
	fmt.Fprintf(sb, "%sTop origins%s\n", bold, reset)
	for _, to := range o.TopOrigins {
		fmt.Fprintf(sb, "  %6d  %s\n", to.Count, truncate(to.Origin, maxLineLen-10))
	}
	if len(o.Scopes) > 0 {
		fmt.Fprintf(sb, "\n%sScopes%s (pprof label \"scope\")\n", bold, reset)
		for _, s := range o.Scopes {
			fmt.Fprintf(sb, "  %-24s goroutines %-5d waiting %-5d leaked %d\n", s.Name, s.Goroutines, s.Waiting, s.Leaked)
		}
	}
	return nil
}

func (t *TUI) renderIssues(ctx context.Context, sb *strings.Builder) error {
	issues, err := t.client.Issues(ctx)
	if err != nil {
		return err
	}
	t.issues = issues
	fmt.Fprintf(sb, "%sIssues%s\n", bold, reset)
	if len(issues) == 0 {
		fmt.Fprintf(sb, "  %sno issues detected%s\n", dim, reset)
	}
	for n, i := range issues {
		g := ""
		if i.Goroutine != nil {
			g = fmt.Sprintf("  → g #%d (G%d)", n+1, *i.Goroutine)
		}
		fmt.Fprintf(sb, "  #%-2d %s%-16s%s %s  %s%s\n", n+1, red, i.Kind, reset, certainty(i.Certainty), i.Summary, g)
		fmt.Fprintf(sb, "      %s%s%s %s\n", dim, truncate(i.Subject, maxLineLen-20), reset, sparkline(i.Trend))
	}
	recs, err := t.client.Recordings(ctx)
	if err == nil && len(recs) > 0 {
		fmt.Fprintf(sb, "\n%sSaved recordings%s %s(export with gorviz-cli export)%s\n", bold, reset, dim, reset)
		for _, r := range recs[:min(len(recs), 5)] {
			fmt.Fprintf(sb, "  %-16s %s  %d chunks  %s\n", r.Kind, r.At.Format(time.TimeOnly), r.Chunks, truncate(r.ID, 60))
		}
	}
	return nil
}

func (t *TUI) renderLogs(ctx context.Context, sb *strings.Builder) error {
	logs, err := t.client.Logs(ctx, serverapi.LogQuery{Query: t.logQuery, Limit: maxRows * 2})
	if err != nil {
		return err
	}
	fmt.Fprintf(sb, "%sLogs%s %s(newest first, query %q)%s\n", bold, reset, dim, t.logQuery, reset)
	for _, l := range logs {
		mark := " "
		if l.TraceLinked {
			mark = green + "G" + reset
		}
		d := ""
		if l.Duration != "" {
			d = dim + " (" + l.Duration + ")" + reset
		}
		fmt.Fprintf(sb, "  %s %s %-5s %s%s\n", l.At.Local().Format("15:04:05.000"), mark, strings.ToUpper(l.Level), truncate(l.Message, maxLineLen-40), d)
	}
	return nil
}

func (t *TUI) renderScheduler(ctx context.Context, sb *strings.Builder) error {
	s, err := t.client.Scheduler(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(sb, "%sRuntime Scheduler%s at the end of the window %s %s(run queues are inferred from the trace)%s\n", bold, reset, s.WindowEnd.Local().Format("15:04:05.000"), dim, reset)
	fmt.Fprintf(sb, "  global run queue  %s\n", goroutineList(s.GlobalRunQueue, 12))
	for _, p := range s.Procs {
		status := fmt.Sprintf("%-8s", p.Status)
		if p.Status == "running" {
			status = green + status + reset
		} else {
			status = dim + status + reset
		}
		m := "-"
		if p.Thread >= 0 {
			m = fmt.Sprintf("M%d", p.Thread)
		}
		fmt.Fprintf(sb, "  P%-3d %s %-5s local run queue %-28s", p.Proc, status, m, goroutineList(p.LocalRunQueue, 4))
		if p.Running >= 0 {
			fmt.Fprintf(sb, " ▶ G%-6d %s", p.Running, shortFn(p.RunningEntry))
			if p.RunningInSyscall {
				fmt.Fprintf(sb, " %s(in syscall)%s", red, reset)
			}
		}
		sb.WriteString("\n")
	}
	slices.SortFunc(s.Waiting, func(a, b api.WaitingJSON) int { return len(b.Goroutines) - len(a.Goroutines) })
	fmt.Fprintf(sb, "  waiting %s(entry function — wait reason)%s\n", dim, reset)
	for _, w := range s.Waiting[:min(len(s.Waiting), 8)] {
		fmt.Fprintf(sb, "    %5d  %s — %s\n", len(w.Goroutines), shortFn(w.Entry), w.Reason)
	}
	fmt.Fprintf(sb, "  free G list  exited in window: %d\n\n", s.Exited)
	fmt.Fprintf(sb, "%sP timeline%s %s(color = goroutine, · = idle)%s\n", bold, reset, dim, reset)
	for _, l := range s.Lanes {
		fmt.Fprintf(sb, "  P%-3d ", l.Proc)
		for i, b := range l.Busy {
			sb.WriteString(bucketCell(b, l.Goroutines[i]))
		}
		fmt.Fprintf(sb, " %3.0f%%\n", l.Utilization*100)
	}
	if st := s.SchedTrace; st != nil {
		fmt.Fprintf(sb, "\n%sruntime-reported (schedtrace)%s global run queue length %d   idle P %d/%d   M %d\n", bold, reset, st.GlobalRunQueue, st.IdleProcs, st.GOMAXPROCS, st.Threads)
	}
	return nil
}

func (t *TUI) renderGoroutine(ctx context.Context, sb *strings.Builder) error {
	g, err := t.client.Goroutine(ctx, t.g, t.gAt, t.gFrom)
	if err != nil {
		return err
	}
	fmt.Fprintf(sb, "%sGoroutine G%d%s %s  %s%s – %s%s\n\n", bold, g.Goroutine, reset, shortFn(g.Entry), dim, g.From.Local().Format("15:04:05.000"), g.To.Local().Format("15:04:05.000"), reset)
	if g.Primary == "" {
		fmt.Fprintf(sb, "  %sno record of this goroutine in the interval%s\n", dim, reset)
		return nil
	}
	fmt.Fprintf(sb, "  diagnosis   %s%s%s %s\n", cyan+bold, g.Primary, reset, certainty(g.Certainty))
	fmt.Fprintf(sb, "  stopped in  %s\n  location    %s\n  wait reason %s\n  created by  %s\n", g.Function, g.Location, g.WaitReason, g.CreatedBy)
	if g.Leaked {
		fmt.Fprintf(sb, "  %sLEAK FACT%s\n", red, reset)
	}
	fmt.Fprintf(sb, "\n%sEvidence: time by state%s %s(unrecorded %s)%s\n", bold, reset, dim, g.Unrecorded, reset)
	for _, c := range domain.PrimaryCauses {
		if d, ok := g.Breakdown[string(c)]; ok {
			fmt.Fprintf(sb, "  %-9s %s\n", c, d)
		}
	}
	fmt.Fprintf(sb, "\n  %sGMP replay: %s%s%s\n", dim, t.server, g.EvidenceURL, reset)
	return nil
}

func certainty(c string) string {
	if c == string(domain.CertaintyFact) {
		return green + "FACT" + reset
	}
	return yellow + "INFERENCE" + reset
}

func mib(b uint64) string {
	if b == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20))
}

func shortFn(fn string) string {
	if fn == "" {
		return "?"
	}
	return domain.ShortFunctionName(fn)
}

func goroutineList(gs []int64, max int) string {
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

func bucketCell(busy float64, g int64) string {
	var r string
	switch {
	case busy == 0:
		return dim + "·" + reset
	case busy >= 0.66:
		r = "█"
	case busy >= 0.33:
		r = "▓"
	default:
		r = "░"
	}
	return fmt.Sprintf("\x1b[38;5;%dm%s%s", goroutineColors[int(g)%len(goroutineColors)], r, reset)
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

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}
