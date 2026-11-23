package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const (
	schedulerDefault = "-10s"
	// schedulerPoll は最新を追う再生に、新しいチャンクが届いたかを見に行く間隔
	schedulerPoll = 2 * time.Second
)

var schedulerPresets = []preset{{"latest 5s", "-5s"}, {"latest 10s", schedulerDefault}, {"latest 30s", "-30s"}, {"latest 1m", "-1m"}}

type GetSchedulerHandler struct {
	nav    navigator
	chunks *usecase.ListTraceChunksUsecase
}

func NewGetSchedulerHandler(services *usecase.ListServicesUsecase, chunks *usecase.ListTraceChunksUsecase) *GetSchedulerHandler {
	return &GetSchedulerHandler{nav: navigator{services: services}, chunks: chunks}
}

// latestLength は最新を追うときの長さ。from が今からの相対ならその長さ
func latestLength(tr timeRange) time.Duration {
	if d, err := time.ParseDuration(tr.FromRaw); err == nil && d < 0 {
		return -d
	}
	return 10 * time.Second
}

func (h *GetSchedulerHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	p := pathInstance(r)
	q := r.URL.Query()
	tr, err := parseRange(q, now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	base, _, err := h.nav.base(r.Context(), "Scheduler "+string(p.instance), p.service, p.instance, tabScheduler)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	live := tr.Live()
	in := usecase.ListTraceChunksUsecaseInput{Service: p.service, Instance: p.instance, From: tr.From, To: tr.To, Now: now}
	if live {
		in.From, in.To = time.Time{}, time.Time{}
	}
	out, err := h.chunks.Exec(r.Context(), in)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	if length := latestLength(tr); live && length != out.To.Sub(out.From) {
		in.From, in.To = out.To.Add(-length), out.To
		if out, err = h.chunks.Exec(r.Context(), in); err != nil {
			middleware.SetError(r, err)
			return
		}
	}
	action := paths.Scheduler(p.service, p.instance, time.Time{}, time.Time{})
	vm := pages.SchedulerViewModel{Base: base, Instance: string(p.instance), Picker: picker(action, nil, tr, out.From, out.To, schedulerPresets, schedulerDefault), Past: !live, LatestURL: action}
	props := components.GMPReplayProps{Live: live, GoroutineURL: goroutineTemplate(p.service, p.instance), Autoplay: q.Get("autoplay") == "1"}
	if !live {
		length := out.To.Sub(out.From)
		props.NextURL = withQuery(action, shiftQuery(out.From, out.To, length))
		props.PrevURL = withQuery(action, shiftQuery(out.From, out.To, -length))
	}
	view := replayView(p, out, props)
	vm.Replay, vm.FromPreserved, vm.Unavailable, vm.Nearby, vm.LeakNote = view.replay, out.FromPreserved, view.unavailable, view.nearby, view.leakNote
	if live && out.Found {
		vm.StreamURL = withQuery(action+"/stream", url.Values{"after": {rfc(out.Chunk.EndedAt())}})
	}
	render(rw, r, pages.Scheduler(vm))
}

type replayParts struct {
	replay      *components.GMPReplayViewModel
	unavailable []components.UnavailableViewModel
	nearby      []components.LinkItemViewModel
	leakNote    string
}

func replayView(p instancePath, out usecase.ListTraceChunksUsecaseOutput, props components.GMPReplayProps) replayParts {
	v := replayParts{unavailable: unavailableViews(out.Unavailable), nearby: nearbyLinks(p.service, p.instance, out.NearbyTraces)}
	if out.FromPreserved {
		v.unavailable = nil
	}
	if !out.Found {
		return v
	}
	v.replay = &components.GMPReplayViewModel{Props: propsJSON(props), Window: replayWindow(out), FeedID: partials.ReplayFeed}
	if !out.LeakCheckAt.IsZero() {
		v.leakNote = "Leaked goroutines are marked from the leak check at " + dateTime(out.LeakCheckAt) + "."
	}
	return v
}

// goroutineTemplate は再生で G を選んだときに開く Goroutine。{goroutine} はアイランドが埋める
func goroutineTemplate(s observation.ServiceName, i observation.InstanceName) string {
	return strings.Replace(paths.Goroutine(s, i, 1, time.Time{}, time.Time{}), "/goroutines/1", "/goroutines/{goroutine}", 1)
}

// GetSchedulerStreamHandler は最新を追っている再生に、新しく届いたチャンクを SSE の turbo-stream で足していく
type GetSchedulerStreamHandler struct {
	chunks *usecase.ListTraceChunksUsecase
}

func NewGetSchedulerStreamHandler(chunks *usecase.ListTraceChunksUsecase) *GetSchedulerStreamHandler {
	return &GetSchedulerStreamHandler{chunks: chunks}
}

func (h *GetSchedulerStreamHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	p := pathInstance(r)
	cursor, err := parseTime(r.URL.Query().Get("after"), "after", time.Now())
	if err != nil || cursor.IsZero() {
		middleware.SetError(r, domain.ErrInvalidArgument.With("after must be the end of the replayed window (RFC 3339)"))
		return
	}
	sse, err := turbo.NewStreamSSEWriter(rw)
	if err != nil {
		middleware.SetError(r, domain.ErrInternal.Wrap(err, "open the scheduler stream"))
		return
	}
	streamEvery(r.Context(), schedulerPoll, func(ctx context.Context) error {
		now := time.Now()
		if !cursor.Before(now) {
			return nil
		}
		out, err := h.chunks.Exec(ctx, usecase.ListTraceChunksUsecaseInput{Service: p.service, Instance: p.instance, From: cursor, To: now, Now: now})
		if err != nil {
			return err
		}
		if !out.Found || out.FromPreserved || !out.Chunk.EndedAt().After(cursor) {
			return sse.Heartbeat()
		}
		cursor = out.Chunk.EndedAt()
		return sse.Write(ctx, partials.ReplayWindowStream(replayWindow(out)))
	})
}
