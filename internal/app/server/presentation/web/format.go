package web

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func clock(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("15:04:05")
}

func clockMilli(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("15:04:05.000")
}

func dateTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func dateTimeMilli(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04:05.000")
}

func rfc(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// span は期間を「始まり → 終わり (長さ)」で見せる。同じ日なら終わりの日付を省く
func span(from, to time.Time) string {
	end := dateTime(to)
	if from.Local().Format("20060102") == to.Local().Format("20060102") {
		end = clock(to)
	}
	return dateTime(from) + " → " + end + " (" + duration(to.Sub(from)) + ")"
}

func spanMilli(from, to time.Time) string {
	end := dateTimeMilli(to)
	if from.Local().Format("20060102") == to.Local().Format("20060102") {
		end = clockMilli(to)
	}
	return dateTimeMilli(from) + " → " + end + " (" + duration(to.Sub(from)) + ")"
}

// duration は時間の長さを桁に合わせて丸めて書く (1.25ms・3.4s・2h5m)
func duration(d time.Duration) string {
	switch {
	case d <= 0:
		return "0"
	case d < time.Microsecond:
		return strconv.FormatInt(int64(d), 10) + "ns"
	case d < time.Millisecond:
		return trimFloat(float64(d)/float64(time.Microsecond), 1) + "µs"
	case d < time.Second:
		return trimFloat(float64(d)/float64(time.Millisecond), 2) + "ms"
	case d < time.Minute:
		return trimFloat(d.Seconds(), 2) + "s"
	case d < time.Hour:
		return d.Round(time.Second).String()
	case d < 48*time.Hour:
		return strings.TrimSuffix(d.Round(time.Minute).String(), "0s")
	}
	return strconv.Itoa(int(d.Hours()/24)) + "d"
}

func trimFloat(v float64, prec int) string {
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func percent(ratio float64) string {
	switch {
	case ratio <= 0:
		return "0%"
	case ratio < 0.001:
		return "<0.1%"
	case ratio < 0.1:
		return trimFloat(ratio*100, 1) + "%"
	}
	return strconv.Itoa(int(ratio*100+0.5)) + "%"
}

func bytesOf(b float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if i == 0 {
		return strconv.Itoa(int(b)) + "B"
	}
	return trimFloat(b, 1) + units[i]
}

func count(n float64) string {
	switch {
	case n >= 1e6:
		return trimFloat(n/1e6, 1) + "M"
	case n >= 1e4:
		return trimFloat(n/1e3, 1) + "k"
	case n == float64(int64(n)):
		return strconv.FormatInt(int64(n), 10)
	}
	return fmt.Sprintf("%.2f", n)
}

func short(fn string) string {
	if i := strings.LastIndex(fn, "/"); i >= 0 {
		return fn[i+1:]
	}
	return fn
}
