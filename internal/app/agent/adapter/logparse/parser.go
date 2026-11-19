package logparse

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

var (
	// slog の TextHandler: time=2026-11-15T11:29:52.123+09:00 level=INFO msg="..." key=value
	textKVRe = regexp.MustCompile(`(\w+)=("(?:[^"\\]|\\.)*"|\S*)`)
	// 標準の log: 2026/11/15 11:29:52 message / 2026/11/15 11:29:52.123456 message
	stdLogRe = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?) (.*)$`)
	// slog の既定ロガー (log 経由): 2026/11/15 11:29:52 INFO message key=value
	stdLevelRe = regexp.MustCompile(`^(DEBUG|INFO|WARN|ERROR)\s+(.*)$`)
)

// Parser は slog (JSON / テキスト)・zap・zerolog・logrus (JSON / テキスト) と標準の log の形式を読む。どれにも当てはまらない行は、受け取った時刻の素のメッセージとして扱う。
type Parser struct {
	durationKeys map[string]bool
}

func NewParser(cfg *config.Config) *Parser {
	keys := make(map[string]bool, len(cfg.DurationKeys))
	for _, k := range cfg.DurationKeys {
		keys[k] = true
	}
	return &Parser{durationKeys: keys}
}

func (p *Parser) Parse(raw, source string, receivedAt time.Time) model.LogLine {
	line := model.LogLine{At: receivedAt, Message: raw, Raw: raw, Source: source}
	trimmed := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(trimmed, "{"):
		p.parseJSON(&line, trimmed)
	case strings.HasPrefix(trimmed, "time="):
		p.parseText(&line, trimmed)
	default:
		p.parseStdLog(&line, trimmed)
	}
	return line
}

func (p *Parser) parseJSON(line *model.LogLine, s string) {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return
	}
	if v, ok := m["time"].(string); ok {
		setTime(line, time.RFC3339Nano, v)
	}
	// zap の既定 (NewProductionEncoderConfig) は ts に UNIX 秒を小数で書く
	if v, ok := m["ts"].(float64); ok {
		sec, frac := math.Modf(v)
		line.At, line.TimeFromLog = time.Unix(int64(sec), int64(frac*1e9)), true
	}
	if v, ok := m["level"].(string); ok {
		line.Level = toLevel(v)
	}
	if v, ok := m["msg"].(string); ok {
		line.Message = v
	}
	// zerolog の既定のキー
	if v, ok := m["message"].(string); ok {
		line.Message = v
	}
	if v, ok := m[model.LogSeqAttrKey].(float64); ok {
		line.TraceSeq, line.HasTraceSeq = uint64(v), true
	}
	for k, v := range m {
		if !p.durationKeys[k] {
			continue
		}
		switch v := v.(type) {
		case float64:
			// slog の JSONHandler は time.Duration をナノ秒の数値で書く
			line.Duration, line.HasDuration = time.Duration(v), true
		case string:
			setDuration(line, v)
		}
	}
}

func (p *Parser) parseText(line *model.LogLine, s string) {
	for _, m := range textKVRe.FindAllStringSubmatch(s, -1) {
		key, value := m[1], m[2]
		if unq, err := strconv.Unquote(value); err == nil {
			value = unq
		}
		switch key {
		case "time":
			setTime(line, time.RFC3339Nano, value)
		case "level":
			line.Level = toLevel(value)
		case "msg":
			line.Message = value
		case model.LogSeqAttrKey:
			if n, err := strconv.ParseUint(value, 10, 64); err == nil {
				line.TraceSeq, line.HasTraceSeq = n, true
			}
		default:
			if p.durationKeys[key] {
				setDuration(line, value)
			}
		}
	}
}

func (p *Parser) parseStdLog(line *model.LogLine, s string) {
	m := stdLogRe.FindStringSubmatch(s)
	if m == nil {
		return
	}
	layout := "2006/01/02 15:04:05"
	if strings.Contains(m[1], ".") {
		layout = "2006/01/02 15:04:05.999999"
	}
	if t, err := time.ParseInLocation(layout, m[1], time.Local); err == nil {
		line.At, line.TimeFromLog = t, true
	}
	line.Message = m[2]
	if lv := stdLevelRe.FindStringSubmatch(m[2]); lv != nil {
		line.Level, line.Message = toLevel(lv[1]), lv[2]
		p.parseText(line, lv[2])
		line.Message = strings.TrimSpace(textKVRe.ReplaceAllString(lv[2], ""))
	}
}

func setDuration(line *model.LogLine, v string) {
	if d, err := time.ParseDuration(v); err == nil {
		line.Duration, line.HasDuration = d, true
	}
}

func setTime(line *model.LogLine, layout, v string) {
	if t, err := time.Parse(layout, v); err == nil {
		line.At, line.TimeFromLog = t, true
	}
}

func toLevel(v string) model.LogLevel {
	switch strings.ToLower(v) {
	case "debug":
		return model.LogLevelDebug
	case "info":
		return model.LogLevelInfo
	case "warn", "warning":
		return model.LogLevelWarn
	case "error":
		return model.LogLevelError
	}
	return model.LogLevelUnknown
}
