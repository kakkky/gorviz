package domain

import (
	"strconv"
	"strings"
)

// DefaultPageBytes は返す量の上限を渡されなかったときの上限
const DefaultPageBytes = 16 << 10

// PageCursor は続きの位置。一覧の何件目からか
type PageCursor string

func ParsePageCursor(c PageCursor) (int, error) {
	if c == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimPrefix(string(c), "o"))
	if err != nil || n < 0 {
		return 0, ErrInvalidArgument.With("the cursor is broken: " + string(c))
	}
	return n, nil
}

// Page は after 件目から、大きさの合計が maxBytes に収まるまでを返す。1 件目は収まらなくても返す。続きがあれば次の位置も返す
func Page[T any](items []T, after, maxBytes int, size func(T) int) ([]T, PageCursor) {
	if maxBytes <= 0 {
		maxBytes = DefaultPageBytes
	}
	if after >= len(items) {
		return nil, ""
	}
	total, end := 0, after
	for end < len(items) {
		total += size(items[end])
		if total > maxBytes && end > after {
			break
		}
		end++
	}
	if end < len(items) {
		return items[after:end], PageCursor("o" + strconv.Itoa(end))
	}
	return items[after:end], ""
}

// Candidates は打ち間違いの候補。want を含むもの・want に含まれるもの・先頭が同じものを先に、ほかは並びのまま limit 個
func Candidates(want string, have []string, limit int) []string {
	w := strings.ToLower(want)
	var close, rest []string
	for _, h := range have {
		l := strings.ToLower(h)
		if strings.Contains(l, w) || strings.Contains(w, l) || (len(l) > 0 && len(w) > 0 && l[0] == w[0]) {
			close = append(close, h)
		} else {
			rest = append(rest, h)
		}
	}
	out := append(close, rest...)
	return out[:min(len(out), limit)]
}
