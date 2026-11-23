package domain

import (
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/loggerhook"
)

var (
	// slog の TextHandler・logrus の TextFormatter: time=2026-11-15T11:29:52.123+09:00 level=INFO msg="..." key=value
	textKVRe = regexp.MustCompile(`([\w.\-]+)=("(?:[^"\\]|\\.)*"|\S*)`)
	// 標準の log: 2026/11/15 11:29:52 message / 2026/11/15 11:29:52.123456 message
	stdLogRe = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?) (.*)$`)
	// slog の既定ロガー (log 経由): 2026/11/15 11:29:52 INFO message key=value
	stdLevelRe = regexp.MustCompile(`^(DEBUG|INFO|WARN|ERROR)\s+(.*)$`)
	// zap の console encoder: 2026-11-15T11:29:52.123+0900	INFO	caller	message	{"key": "value"}
	zapConsoleRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\S+)\t([A-Z]+)\t(.*)$`)
)

// ParseLogLine は 1 行を読む。形式は設定させず、行の形から読み分ける。どれにも当てはまらない行は、受け取った時刻の素のメッセージとする。
// durationKeys は所要時間が書かれるキー
func ParseLogLine(raw string, source observation.LogSource, receivedAt time.Time, durationKeys []string) observation.LogLine {
	if source == observation.LogSourceTCP || source == observation.LogSourceUDP {
		raw = unwrapRouterRecord(raw)
	}
	line := observation.LogLine{At: receivedAt, Message: raw, Raw: raw, Source: source}
	p := logParser{line: &line, durationKeys: durationKeys}
	trimmed := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(trimmed, "{"):
		p.json(trimmed)
	case strings.HasPrefix(trimmed, "time=") || strings.HasPrefix(trimmed, "level="):
		p.text(trimmed)
	case zapConsoleRe.MatchString(trimmed):
		p.zapConsole(trimmed)
	default:
		p.stdLog(trimmed)
	}
	return line
}

// unwrapRouterRecord はログルーター (FireLens の Fluent Bit など) が、アプリの 1 行を log のキーに入れて包んだ記録から、アプリの行を取り出す
func unwrapRouterRecord(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") || !strings.Contains(trimmed, `"log"`) {
		return raw
	}
	var rec map[string]any
	if json.Unmarshal([]byte(trimmed), &rec) != nil {
		return raw
	}
	inner, ok := rec["log"].(string)
	if !ok {
		return raw
	}
	for _, k := range []string{"container_name", "container_id", "source", "date", "stream"} {
		if _, ok := rec[k]; ok {
			return strings.TrimRight(inner, "\r\n")
		}
	}
	return raw
}

type logParser struct {
	line         *observation.LogLine
	durationKeys []string
}

func (p logParser) json(s string) {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return
	}
	if v, ok := m["time"].(string); ok {
		p.setTime(time.RFC3339Nano, v)
	}
	// zap の既定 (NewProductionEncoderConfig) は ts に UNIX 秒を小数で書く
	if v, ok := m["ts"].(float64); ok {
		sec, frac := math.Modf(v)
		p.line.At, p.line.TimeFromLog = time.Unix(int64(sec), int64(frac*1e9)), true
	}
	if v, ok := m["level"].(string); ok {
		p.line.Level = toLevel(v)
	}
	for _, k := range []string{"msg", "message"} {
		if v, ok := m[k].(string); ok {
			p.line.Message = v
			break
		}
	}
	if n, ok := findSeq(m); ok {
		p.line.TraceSeq, p.line.HasTraceSeq = n, true
	}
	for k, v := range m {
		if !slices.Contains(p.durationKeys, k) {
			continue
		}
		switch v := v.(type) {
		case float64:
			// slog の JSONHandler は time.Duration をナノ秒の数値で書く
			p.line.Duration, p.line.HasDuration = time.Duration(v), true
		case string:
			p.setDuration(v)
		}
	}
}

// findSeq は slog の WithGroup の後に書いたログでは番号がグループの中に入るので、入れ子の中も探す
func findSeq(m map[string]any) (uint64, bool) {
	if v, ok := m[loggerhook.SeqKey].(float64); ok && v >= 0 {
		return uint64(v), true
	}
	for _, v := range m {
		if g, ok := v.(map[string]any); ok {
			if n, ok := findSeq(g); ok {
				return n, true
			}
		}
	}
	return 0, false
}

func (p logParser) text(s string) {
	for _, m := range textKVRe.FindAllStringSubmatch(s, -1) {
		key, value := m[1], m[2]
		if unq, err := strconv.Unquote(value); err == nil {
			value = unq
		}
		switch {
		case key == "time":
			p.setTime(time.RFC3339Nano, value)
		case key == "level":
			p.line.Level = toLevel(value)
		case key == "msg":
			p.line.Message = value
		case key == loggerhook.SeqKey || strings.HasSuffix(key, "."+loggerhook.SeqKey):
			if n, err := strconv.ParseUint(value, 10, 64); err == nil {
				p.line.TraceSeq, p.line.HasTraceSeq = n, true
			}
		case slices.Contains(p.durationKeys, key):
			p.setDuration(value)
		}
	}
}

func (p logParser) zapConsole(s string) {
	m := zapConsoleRe.FindStringSubmatch(s)
	p.setTime("2006-01-02T15:04:05.000Z0700", m[1])
	p.line.Level = toLevel(m[2])
	rest := m[3]
	if i := strings.Index(rest, "\t{"); i >= 0 {
		var fields map[string]any
		if json.Unmarshal([]byte(rest[i+1:]), &fields) == nil {
			if n, ok := findSeq(fields); ok {
				p.line.TraceSeq, p.line.HasTraceSeq = n, true
			}
		}
		rest = rest[:i]
	}
	// caller を出す設定では、メッセージの前にタブ区切りで入る
	if _, msg, ok := strings.Cut(rest, "\t"); ok && strings.Contains(rest[:len(rest)-len(msg)], ".go:") {
		rest = msg
	}
	p.line.Message = rest
}

func (p logParser) stdLog(s string) {
	m := stdLogRe.FindStringSubmatch(s)
	if m == nil {
		return
	}
	layout := "2006/01/02 15:04:05"
	if strings.Contains(m[1], ".") {
		layout = "2006/01/02 15:04:05.999999"
	}
	if t, err := time.ParseInLocation(layout, m[1], time.Local); err == nil {
		p.line.At, p.line.TimeFromLog = t, true
	}
	p.line.Message = m[2]
	if lv := stdLevelRe.FindStringSubmatch(m[2]); lv != nil {
		p.text(lv[2])
		p.line.Level = toLevel(lv[1])
		p.line.Message = strings.TrimSpace(textKVRe.ReplaceAllString(lv[2], ""))
	}
}

func (p logParser) setDuration(v string) {
	if d, err := time.ParseDuration(v); err == nil {
		p.line.Duration, p.line.HasDuration = d, true
	}
}

func (p logParser) setTime(layout, v string) {
	if t, err := time.Parse(layout, v); err == nil {
		p.line.At, p.line.TimeFromLog = t, true
	}
}

func toLevel(v string) observation.LogLevel {
	switch strings.ToLower(v) {
	case "trace", "debug":
		return observation.LogLevelDebug
	case "info":
		return observation.LogLevelInfo
	case "warn", "warning":
		return observation.LogLevelWarn
	case "error", "dpanic", "panic", "fatal":
		return observation.LogLevelError
	}
	return observation.LogLevelUnknown
}
