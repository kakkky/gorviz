package web

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// timeRange は query の from・to。どちらも RFC 3339 か今からの相対 (-15m)。To が空なら今まで (最新を追う)
type timeRange struct {
	From    time.Time
	To      time.Time
	FromRaw string
	ToRaw   string
}

// Live は今に合わせて動く期間か。to がなく、from が相対か空
func (t timeRange) Live() bool {
	if t.ToRaw != "" && t.ToRaw != "now" {
		return false
	}
	_, err := time.ParseDuration(t.FromRaw)
	return t.FromRaw == "" || err == nil
}

func parseTime(v, name string, now time.Time) (time.Time, error) {
	switch v {
	case "":
		return time.Time{}, nil
	case "now":
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

func parseRange(q url.Values, now time.Time) (timeRange, error) {
	r := timeRange{FromRaw: q.Get("from"), ToRaw: q.Get("to")}
	var err error
	if r.From, err = parseTime(r.FromRaw, "from", now); err != nil {
		return timeRange{}, err
	}
	if r.To, err = parseTime(r.ToRaw, "to", now); err != nil {
		return timeRange{}, err
	}
	return r, nil
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

// parsePeriodQuery は goroutine の調べる期間の入口 (from・to / log / issue と occurrence / woken)
func parsePeriodQuery(q url.Values, now time.Time) (usecase.PeriodQuery, error) {
	r, err := parseRange(q, now)
	if err != nil {
		return usecase.PeriodQuery{}, err
	}
	occ, err := parseTime(q.Get("occurrence"), "occurrence", now)
	if err != nil {
		return usecase.PeriodQuery{}, err
	}
	woken, err := parseGoroutine(q.Get("woken"), "woken")
	if err != nil {
		return usecase.PeriodQuery{}, err
	}
	return usecase.PeriodQuery{From: r.From, To: r.To, Log: domain.LogID(q.Get("log")), Issue: q.Get("issue"), Occurrence: occ, Woken: woken}, nil
}

func parseLogFilter(q url.Values) (domain.LogFilter, error) {
	f := domain.LogFilter{Level: observation.LogLevel(strings.ToUpper(q.Get("level"))), Instance: observation.InstanceName(q.Get("instance")), Text: q.Get("q")}
	switch f.Level {
	case "", observation.LogLevelDebug, observation.LogLevelInfo, observation.LogLevelWarn, observation.LogLevelError:
	default:
		return domain.LogFilter{}, domain.ErrInvalidArgument.With("level must be DEBUG, INFO, WARN or ERROR")
	}
	for _, a := range q["attr"] {
		if strings.TrimSpace(a) == "" {
			continue
		}
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

type instancePath struct {
	service  observation.ServiceName
	instance observation.InstanceName
}

func pathInstance(r interface{ PathValue(string) string }) instancePath {
	return instancePath{service: observation.ServiceName(r.PathValue("service")), instance: observation.InstanceName(r.PathValue("instance"))}
}
