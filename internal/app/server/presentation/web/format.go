package web

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// at はセッションの始まりからの経過時間。URL の from・to にもこの形で載せる
func at(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64) + "s"
}

// span は期間を「始まり → 終わり (長さ)」で見せる
func span(from, to time.Duration) string {
	return at(from) + " → " + at(to) + " (" + duration(to-from) + ")"
}

func duration(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	return domain.FormatDuration(d)
}

func trimFloat(v float64, prec int) string {
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }

func count(v float64) string {
	switch {
	case v >= 1e6:
		return trimFloat(v/1e6, 1) + "M"
	case v >= 1e4:
		return trimFloat(v/1e3, 1) + "k"
	}
	return trimFloat(v, 1)
}

func percent(v float64) string {
	if v > 0 && v < 0.001 {
		return "<0.1%"
	}
	return trimFloat(v*100, 1) + "%"
}

func bytesOf(v float64) string {
	units := []string{"B", "KiB", "MiB", "GiB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return trimFloat(v, 1) + units[i]
}

func gid(g domain.GoroutineID) string {
	return fmt.Sprintf("G%d", g)
}

func issueURL(id domain.IssueID, n int) string {
	if n > 0 {
		return fmt.Sprintf("/issues/%s?occurrence=%d", id, n)
	}
	return "/issues/" + string(id)
}

func goroutineURL(id domain.GoroutineID, from, to time.Duration) string {
	return fmt.Sprintf("/goroutines/%d?from=%s&to=%s", id, at(from), at(to))
}

func schedulerURL(from, to time.Duration) string {
	return fmt.Sprintf("/scheduler?from=%s&to=%s", at(from), at(to))
}
