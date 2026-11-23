// Package api は HTTP API (gorviz-cli・gorviz-mcp が使う) の handler
package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/response"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// parseTime は RFC 3339 か、今からの相対 (-15m・-2h) を読む
func parseTime(q url.Values, name string, now time.Time) (time.Time, error) {
	v := q.Get(name)
	if v == "" {
		return time.Time{}, nil
	}
	if v == "now" {
		return now, nil
	}
	if d, err := time.ParseDuration(v); err == nil {
		return now.Add(d), nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, domain.ErrInvalidArgument.With(name + " must be RFC 3339 (2026-11-21T14:03:00Z) or relative to now (-15m)")
	}
	return t, nil
}

func parseInt(q url.Values, name string) (int, error) {
	v := q.Get(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, domain.ErrInvalidArgument.With(name + " must be a non-negative integer")
	}
	return n, nil
}

func parseGoroutine(v, name string) (observation.GoroutineID, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, domain.ErrInvalidArgument.With(name + " must be a goroutine id")
	}
	return observation.GoroutineID(n), nil
}

func parseFilter(q url.Values) (domain.LogFilter, error) {
	f := domain.LogFilter{Level: observation.LogLevel(strings.ToUpper(q.Get("level"))), Instance: observation.InstanceName(q.Get("instance")), Text: q.Get("q")}
	switch f.Level {
	case "", observation.LogLevelDebug, observation.LogLevelInfo, observation.LogLevelWarn, observation.LogLevelError:
	default:
		return domain.LogFilter{}, domain.ErrInvalidArgument.With("level must be DEBUG, INFO, WARN or ERROR")
	}
	for _, a := range q["attr"] {
		k, v, ok := strings.Cut(a, "=")
		if !ok || k == "" {
			return domain.LogFilter{}, domain.ErrInvalidArgument.With("attr must be key=value")
		}
		if f.Attrs == nil {
			f.Attrs = map[string]string{}
		}
		f.Attrs[k] = v
	}
	return f, nil
}

func parseRange(q url.Values, now time.Time) (time.Time, time.Time, error) {
	from, err := parseTime(q, "from", now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := parseTime(q, "to", now)
	return from, to, err
}

// parsePeriodQuery は goroutine の調べる期間の入口 (from・to / log / issue と occurrence / woken)
func parsePeriodQuery(q url.Values, now time.Time) (usecase.PeriodQuery, error) {
	from, to, err := parseRange(q, now)
	if err != nil {
		return usecase.PeriodQuery{}, err
	}
	occ, err := parseTime(q, "occurrence", now)
	if err != nil {
		return usecase.PeriodQuery{}, err
	}
	woken, err := parseGoroutine(q.Get("woken"), "woken")
	if err != nil {
		return usecase.PeriodQuery{}, err
	}
	return usecase.PeriodQuery{From: from, To: to, Log: domain.LogID(q.Get("log")), Issue: q.Get("issue"), Occurrence: occ, Woken: woken}, nil
}

type instanceParams struct {
	service  observation.ServiceName
	instance observation.InstanceName
}

func pathInstance(r *http.Request) instanceParams {
	return instanceParams{service: observation.ServiceName(r.PathValue("service")), instance: observation.InstanceName(r.PathValue("instance"))}
}

func write(rw http.ResponseWriter, r *http.Request, v any) {
	if err := response.Write(rw, r, http.StatusOK, v); err != nil {
		middleware.SetError(r, domain.ErrInternal.Wrap(err, "write a response"))
	}
}
