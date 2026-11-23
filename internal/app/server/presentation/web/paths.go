package web

import (
	"net/url"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// paths は GUI の中の URL。通知や HTTP API が返す URL (domain.GUI) と同じ形にする
var paths = domain.GUI{}

func withQuery(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

func servicePath(s observation.ServiceName, page string) string {
	return "/services/" + url.PathEscape(string(s)) + "/" + page
}

func rangeQuery(from, to time.Time) url.Values {
	q := url.Values{}
	if !from.IsZero() {
		q.Set("from", rfc(from))
	}
	if !to.IsZero() {
		q.Set("to", rfc(to))
	}
	return q
}

// goroutineFromIssue は Issue の発生から開く Goroutine。調べる期間は発生の前後になる
func goroutineFromIssue(s observation.ServiceName, i observation.InstanceName, g observation.GoroutineID, issue string, o domain.Occurrence) string {
	base := paths.Goroutine(s, i, g, time.Time{}, time.Time{})
	return withQuery(base, url.Values{"issue": {issue}, "occurrence": {rfc(o.StartedAt)}})
}

func goroutineFromLog(s observation.ServiceName, i observation.InstanceName, g observation.GoroutineID, log domain.LogID) string {
	return withQuery(paths.Goroutine(s, i, g, time.Time{}, time.Time{}), url.Values{"log": {string(log)}})
}

// goroutineWoken は待ちを起こした goroutine の Goroutine。調べる期間は起こされた側が待っていた区間になる
func goroutineWoken(s observation.ServiceName, i observation.InstanceName, waker, woken observation.GoroutineID, from, to time.Time) string {
	q := rangeQuery(from, to)
	q.Set("woken", strconv.FormatInt(int64(woken), 10))
	return withQuery(paths.Goroutine(s, i, waker, time.Time{}, time.Time{}), q)
}
