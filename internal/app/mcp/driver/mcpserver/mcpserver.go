// Package mcpserver は stdio で 1 行 1 メッセージの JSON-RPC を待ち受ける
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/kakkky/gorviz/internal/app/mcp/presentation/handler"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      json.RawMessage        `json:"id"`
	Result  any                    `json:"result,omitempty"`
	Error   *handler.ProtocolError `json:"error,omitempty"`
}

type MCPServer struct {
	in      io.ReadCloser
	out     io.Writer
	handler *handler.ToolHandler
	mu      sync.Mutex
}

func NewMCPServer(in io.ReadCloser, out io.Writer, h *handler.ToolHandler) *MCPServer {
	return &MCPServer{in: in, out: out, handler: h}
}

// Run は in を読み切るか ctx が終わるまで返らない。読んでいる途中の in は閉じて抜ける
func (s *MCPServer) Run(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() { s.in.Close() })
	defer stop()
	sc := bufio.NewScanner(s.in)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			if err := s.write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: handler.ErrParse}); err != nil {
				return err
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		result, perr := s.handler.Handle(ctx, req.Method, req.Params)
		if err := s.write(response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: perr}); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return sc.Err()
}

func (s *MCPServer) write(r response) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.NewEncoder(s.out).Encode(r)
}
