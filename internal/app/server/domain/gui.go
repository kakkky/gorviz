package domain

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// GUI は同じものを開く GUI の URL を作る。Base は利用者がブラウザで開く server の URL
type GUI struct{ Base string }

func (g GUI) url(path string, q url.Values) string {
	u := strings.TrimSuffix(g.Base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

func esc(s string) string { return url.PathEscape(s) }

func period(from, to time.Time) url.Values {
	q := url.Values{}
	if !from.IsZero() {
		q.Set("from", from.UTC().Format(time.RFC3339Nano))
	}
	if !to.IsZero() {
		q.Set("to", to.UTC().Format(time.RFC3339Nano))
	}
	return q
}

func (g GUI) Overview(s observation.ServiceName) string {
	return g.url("/services/"+esc(string(s))+"/overview", nil)
}

func (g GUI) Issue(s observation.ServiceName, issue string) string {
	return g.url("/services/"+esc(string(s))+"/issues/"+esc(issue), nil)
}

func (g GUI) Log(s observation.ServiceName, id LogID) string {
	return g.url("/services/"+esc(string(s))+"/logs", url.Values{"log": {string(id)}})
}

func (g GUI) Scheduler(s observation.ServiceName, i observation.InstanceName, from, to time.Time) string {
	return g.url("/services/"+esc(string(s))+"/instances/"+esc(string(i))+"/scheduler", period(from, to))
}

func (g GUI) Goroutine(s observation.ServiceName, i observation.InstanceName, gid observation.GoroutineID, from, to time.Time) string {
	return g.url("/services/"+esc(string(s))+"/instances/"+esc(string(i))+"/goroutines/"+strconv.FormatInt(int64(gid), 10), period(from, to))
}
