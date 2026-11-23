// Package accessor は保存先のディレクトリを、domain の読み書きの操作に対応づける。パスの決め方はここだけが持つ
package accessor

import (
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const (
	servicesDir    = "services"
	checkpointsDir = "server/checkpoints"
	hourFormat     = "2006-01-02T15"
	dayFormat      = "2006-01-02"
)

// pathName は名前を 1 つのパスの要素にする。agent が付けた名前に / や .. が入っていても、別の場所を指さないため
func pathName(s string) string {
	e := url.PathEscape(s)
	if e == "." || e == ".." || e == "" {
		e = strings.ReplaceAll(e, ".", "%2E") + "%00"
	}
	return e
}

func unPathName(s string) string {
	if strings.HasSuffix(s, "%00") {
		s = strings.TrimSuffix(s, "%00")
	}
	u, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return u
}

func serviceDir(s observation.ServiceName) string {
	return path.Join(servicesDir, pathName(string(s)), "service")
}

func instanceDir(s observation.ServiceName, i observation.InstanceName) string {
	return path.Join(servicesDir, pathName(string(s)), "instances", pathName(string(i)))
}

func periodName(k domain.DataKind, t time.Time) string {
	if k.Period() == time.Hour {
		return k.PeriodStart(t).Format(hourFormat)
	}
	return k.PeriodStart(t).Format(dayFormat)
}

func parsePeriod(k domain.DataKind, name string) (time.Time, bool) {
	name, _, _ = strings.Cut(name, ".")
	f := dayFormat
	if k.Period() == time.Hour {
		f = hourFormat
	}
	t, err := time.Parse(f, name)
	return t, err == nil
}

func serviceFile(s observation.ServiceName, k domain.DataKind, t time.Time, ext string) string {
	return path.Join(serviceDir(s), string(k), periodName(k, t)+ext)
}

func instanceFile(s observation.ServiceName, i observation.InstanceName, k domain.DataKind, t time.Time, ext string) string {
	return path.Join(instanceDir(s, i), string(k), periodName(k, t)+ext)
}

// periods は [from, to] に重なるファイルの時間帯の始まり。from の前の時間帯も含める (チャンクが時間帯の境目をまたぐため)
func periods(k domain.DataKind, from, to time.Time, before int) []time.Time {
	var out []time.Time
	for t := k.PeriodStart(from).Add(-time.Duration(before) * k.Period()); !t.After(to); t = t.Add(k.Period()) {
		out = append(out, t)
	}
	return out
}

func checkpointFile(s observation.ServiceName) string {
	return path.Join(checkpointsDir, pathName(string(s))+".json")
}
