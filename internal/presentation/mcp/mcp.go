// Package mcp は LLM エージェント向けの MCP サーバ (stdio)。画面と同じ診断を、server の API から取って構造化データ (JSON) で返す。
// 読み取りの操作だけを持つ。LLM が一度に読める量には限りがあるので、一覧は要約を返し、詳細は ID や時刻で追加で取らせる
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/adapter/serverapi"
)

const protocolVersion = "2025-06-18"

type Server struct {
	client *serverapi.Client
	server string
}

func New(client *serverapi.Client, server string) *Server {
	return &Server{client: client, server: server}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve は 1 行 1 メッセージの JSON-RPC を r から読み、w に返す。r を読み切るまで返らない
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			if err := enc.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}}); err != nil {
				return err
			}
			continue
		}
		// id のないものは通知なので返事をしない
		if len(req.ID) == 0 {
			continue
		}
		result, rerr := s.handle(ctx, req)
		if err := enc.Encode(response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rerr}); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "gorviz", "version": "0.1.0"},
			"instructions": "gorviz diagnoses what goroutines of a Go application were stopped on. Start with list_issues, then diagnose_goroutine for the goroutine of an issue. " +
				"Every diagnosis has a certainty: FACT is read directly from the runtime, INFERENCE is gorviz's guess. gorviz does not tell the cause in the code; read the code at the reported location to find it.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid params"}
		}
		out, err := s.call(ctx, p.Name, p.Arguments)
		if err != nil {
			return toolResult(err.Error(), true), nil
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return toolResult(err.Error(), true), nil
		}
		return toolResult(string(b), false), nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isError}
}

func (s *Server) call(ctx context.Context, name string, args map[string]any) (any, error) {
	c := s.client
	if agent := str(args, "agent"); agent != "" {
		c = serverapi.NewClient(s.server, agent)
	}
	switch name {
	case "list_agents":
		return c.Agents(ctx)
	case "get_overview":
		o, err := c.Overview(ctx)
		if err != nil {
			return nil, err
		}
		// 推移の配列は長いので、最後の値と最小・最大だけに縮める
		history := o.GoroutineHistory
		o.GoroutineHistory = nil
		return map[string]any{"overview": o, "goroutineHistory": summarize(history)}, nil
	case "list_issues":
		return c.Issues(ctx)
	case "diagnose_goroutine":
		id, err := strconv.ParseInt(str(args, "goroutine"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("goroutine must be a goroutine id: %w", err)
		}
		at, err := parseTime(str(args, "at"))
		if err != nil {
			return nil, err
		}
		var from time.Time
		if f := str(args, "from"); f != "" {
			if from, err = parseTime(f); err != nil {
				return nil, err
			}
		}
		g, err := c.Goroutine(ctx, id, at, from)
		if err != nil {
			return nil, err
		}
		g.EvidenceURL = s.server + g.EvidenceURL
		return g, nil
	case "search_logs":
		limit, _ := strconv.Atoi(str(args, "limit"))
		if limit <= 0 {
			limit = 20
		}
		return c.Logs(ctx, serverapi.LogQuery{Query: str(args, "query"), Level: str(args, "level"), Limit: limit})
	case "list_recordings":
		return c.Recordings(ctx)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func str(args map[string]any, key string) string {
	switch v := args[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

// parseTime は RFC 3339 か Unix ナノ秒を読む。list_issues の at / from はどちらでも渡せる
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Now().Add(-3 * time.Second), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("time must be RFC 3339 or unix nanoseconds: %q", s)
	}
	return time.Unix(0, n), nil
}

func summarize(values []int) map[string]int {
	if len(values) == 0 {
		return map[string]int{}
	}
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo, hi = min(lo, v), max(hi, v)
	}
	return map[string]int{"samples": len(values), "min": lo, "max": hi, "latest": values[len(values)-1]}
}

var agentProp = map[string]any{"type": "string", "description": "name of the agent (default: the first connected one). See list_agents"}

var tools = []map[string]any{
	{
		"name": "list_agents", "description": "List the agents (observed Go processes) connected to the gorviz server.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		"name": "get_overview", "description": "Summary of the process: goroutine count and trend, open issues, errors and slow requests in the last minute, goroutines waiting for a P, heap and GC.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"agent": agentProp}},
	},
	{
		"name": "list_issues", "description": "Detected issues (LEAK, ERROR_SPIKE, SLOW_REQUEST, CPU_WAIT_SPIKE, MEMORY_PRESSURE) with certainty (FACT / INFERENCE). An issue with a goroutine can be diagnosed with diagnose_goroutine using its goroutine, at and from.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"agent": agentProp}},
	},
	{
		"name": "diagnose_goroutine", "description": "Diagnose one goroutine over an interval: its primary cause (RUNNING, CPU_WAIT, BLOCKED, IO_WAIT, CGO, SLEEP, GC), where it stopped (file:line), and the time by state as evidence. evidenceUrl opens the GMP replay for a human.",
		"inputSchema": map[string]any{
			"type": "object", "required": []string{"goroutine"},
			"properties": map[string]any{
				"agent":     agentProp,
				"goroutine": map[string]any{"type": "integer", "description": "goroutine id"},
				"at":        map[string]any{"type": "string", "description": "the moment to look at, RFC 3339 or unix nanoseconds (default: now - 3s)"},
				"from":      map[string]any{"type": "string", "description": "start of the interval (e.g. the start of a request); when omitted, 250ms around at"},
			},
		},
	},
	{
		"name": "search_logs", "description": "Search the target's logs, newest first.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"agent": agentProp,
			"query": map[string]any{"type": "string"},
			"level": map[string]any{"type": "string", "enum": []string{"", "debug", "info", "warn", "error"}},
			"limit": map[string]any{"type": "integer", "description": "default 20, up to 1000"},
		}},
	},
	{
		"name": "list_recordings", "description": "Recordings the server saved around issues. They stay after the agent's ring buffer drops them.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"agent": agentProp}},
	},
}
