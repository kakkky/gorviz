// Package handler は 1 つの操作を 1 つの MCP の tool として出す。server の応答は解釈せず、そのまま tool の結果にする
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/kakkky/gorviz/internal/docs"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

const (
	protocolVersion = "2025-06-18"
	listDocsTool    = "list-docs"
	getDocTool      = "get-doc"
)

type ProtocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var (
	ErrParse          = &ProtocolError{Code: -32700, Message: "parse error"}
	errInvalidParams  = &ProtocolError{Code: -32602, Message: "invalid params"}
	errMethodNotFound = &ProtocolError{Code: -32601, Message: "method not found"}
)

type ToolHandler struct {
	client *serverapi.Client
}

func NewToolHandler(client *serverapi.Client) *ToolHandler {
	return &ToolHandler{client: client}
}

func (h *ToolHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, *ProtocolError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "gorviz", "version": "0.1.0"},
			"instructions":    instructions(),
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools()}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, errInvalidParams
		}
		return h.call(ctx, p.Name, p.Arguments), nil
	}
	return nil, errMethodNotFound
}

func instructions() string {
	body, _ := docs.Get("getting-started")
	return fmt.Sprintf("調べ始める前に `%s` でドキュメントの一覧を取り、`%s` で getting-started・main-causes・issue-kinds・certainty を読む。\n\n%s", listDocsTool, getDocTool, body)
}

func tools() []map[string]any {
	out := []map[string]any{
		{"name": listDocsTool, "description": "引けるドキュメントの名前と、何が書いてあるか", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
		{"name": getDocTool, "description": "1 つのドキュメントの中身", "inputSchema": map[string]any{
			"type": "object", "required": []string{"name"},
			"properties": map[string]any{"name": map[string]any{"type": "string", "description": "list-docs の名前"}},
		}},
	}
	for _, r := range serverapi.AllRoutes {
		props := map[string]any{}
		required := []string{}
		for _, p := range r.Params {
			if p.Repeated {
				props[p.Name] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
			} else {
				props[p.Name] = map[string]any{"type": "string"}
			}
			if p.Required {
				required = append(required, p.Name)
			}
		}
		body, _ := docs.Operation(r.Name)
		out = append(out, map[string]any{"name": r.Name, "description": body, "inputSchema": map[string]any{"type": "object", "properties": props, "required": required}})
	}
	return out
}

func (h *ToolHandler) call(ctx context.Context, name string, args map[string]any) map[string]any {
	switch name {
	case listDocsTool:
		var text string
		for _, d := range docs.List() {
			text += d.Name + "\t" + d.Summary + "\n"
		}
		return result(text, false)
	case getDocTool:
		body, ok := docs.Get(str(args["name"]))
		if !ok {
			return result(fmt.Sprintf("no document %q. %s lists them", str(args["name"]), listDocsTool), true)
		}
		return result(body, false)
	}
	i := slices.IndexFunc(serverapi.AllRoutes, func(r serverapi.Route) bool { return r.Name == name })
	if i < 0 {
		return result(fmt.Sprintf("unknown tool %q", name), true)
	}
	route := serverapi.AllRoutes[i]
	params := map[string]string{}
	q := serverapi.Query{}
	for _, p := range route.Params {
		var vs []string
		switch v := args[p.Name].(type) {
		case nil:
		case []any:
			for _, e := range v {
				vs = append(vs, str(e))
			}
		default:
			vs = []string{str(v)}
		}
		if len(vs) == 0 {
			if p.Required {
				return result(fmt.Sprintf("%s is required. get-doc operations/%s shows how to use it", p.Name, route.Name), true)
			}
			continue
		}
		if p.In == serverapi.InPath {
			params[p.Name] = vs[0]
			continue
		}
		q[p.Name] = vs
	}
	body, err := h.client.Call(ctx, route, params, q)
	if err != nil {
		return result(explain(err), true)
	}
	return result(string(body), false)
}

func explain(err error) string {
	msg := err.Error()
	if errors.Is(err, serverapi.ErrUnauthorized) {
		body, _ := docs.Get("authentication")
		return msg + "\n\n" + body
	}
	return msg + "\n\n" + listDocsTool + " lists the documents"
}

func result(text string, isError bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isError}
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	return fmt.Sprint(v)
}
