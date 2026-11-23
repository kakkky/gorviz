package logsource

import (
	"regexp"
	"strings"
)

// criLineRe は Kubernetes (CRI) のログファイルの行頭: "2026-11-15T11:29:52.123456789Z stdout F "
var criLineRe = regexp.MustCompile(`^\S+ (?:stdout|stderr) ([PF]) `)

// lineJoiner はファイルごとに、CRI の行頭を剥がし、長い行が P (partial) で分けて書かれたものを 1 行につなぐ
type lineJoiner struct {
	partial  strings.Builder
	maxBytes int
}

// join は 1 行を受け、出せる行があれば返す
func (j *lineJoiner) join(line string) (string, bool) {
	m := criLineRe.FindStringSubmatchIndex(line)
	if m == nil {
		return line, true
	}
	body := line[m[1]:]
	if j.partial.Len()+len(body) <= j.maxBytes {
		j.partial.WriteString(body)
	}
	if line[m[2]:m[3]] == "P" {
		return "", false
	}
	out := j.partial.String()
	j.partial.Reset()
	return out, true
}
