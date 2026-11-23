package api

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

// dur は時間の長さを 1.5ms のような読める形で書く
type dur time.Duration

func (d dur) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

func utc(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

type periodJSON struct {
	From      *time.Time `json:"from"`
	To        *time.Time `json:"to"`
	DecidedBy string     `json:"decidedBy,omitempty"`
}

func toPeriod(p domain.Period) periodJSON {
	return periodJSON{From: utc(p.From), To: utc(p.To), DecidedBy: string(p.By)}
}

type unavailableJSON struct {
	Field         string     `json:"field"`
	Reason        string     `json:"reason"`
	RetainedSince *time.Time `json:"retainedSince"`
}

func toUnavailable(us []usecase.Unavailable) []unavailableJSON {
	out := []unavailableJSON{}
	for _, u := range us {
		out = append(out, unavailableJSON{Field: u.Field, Reason: u.Reason, RetainedSince: utc(u.RetainedSince)})
	}
	return out
}

type preservedJSON struct {
	Kind         string     `json:"kind"`
	Dir          string     `json:"dir"`
	At           *time.Time `json:"at"`
	From         *time.Time `json:"from"`
	To           *time.Time `json:"to"`
	SchedulerURL string     `json:"schedulerUrl"`
}

func toPreserved(gui domain.GUI, s observation.ServiceName, i observation.InstanceName, ps []domain.PreservedTrace) []preservedJSON {
	out := []preservedJSON{}
	for _, p := range ps {
		out = append(out, preservedJSON{Kind: string(p.Kind), Dir: p.Dir, At: utc(p.At), From: utc(p.From), To: utc(p.To), SchedulerURL: gui.Scheduler(s, i, p.From, p.To)})
	}
	return out
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// apiPath は次に呼ぶ API の、パスと query。値を {name} のまま残した引数は、呼ぶ側が埋める
func apiPath(route serverapi.Route, params map[string]string, q url.Values) string {
	p := strings.NewReplacer("%7B", "{", "%7D", "}").Replace(route.Path(params))
	if len(q) > 0 {
		p += "?" + q.Encode()
	}
	return p
}

func diagnosisPath(s observation.ServiceName, i observation.InstanceName, g observation.GoroutineID, q url.Values) string {
	return apiPath(serverapi.DiagnoseGoroutine, map[string]string{"service": string(s), "instance": string(i), "goroutine": strconv.FormatInt(int64(g), 10)}, q)
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

func certaintyNote(c domain.Certainty) string {
	switch c {
	case domain.CertaintyFact:
		return "FACT: read directly from the runtime's judgement or the recorded trace"
	case domain.CertaintyInference:
		return "INFERENCE: inferred by gorviz from trends or comparison"
	}
	return ""
}

func itoa(n int) string { return strconv.Itoa(n) }
