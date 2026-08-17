package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/ops"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

// Options carries dependencies for the MCP server.
type Options struct {
	Config  *config.Config
	DB      *gorm.DB
	Pool    *ssh.Pool
	Catalog *ops.Catalog
}

// RunStdio starts a newline-delimited JSON-RPC MCP server on stdin/stdout.
func RunStdio(opts Options) error {
	cat := opts.Catalog
	if cat == nil {
		cat = ops.New(opts.DB, opts.Pool)
	}
	s := &server{opts: opts, cat: cat}
	return s.serve(os.Stdin, os.Stdout)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type server struct {
	opts Options
	cat  *ops.Catalog
}

func (s *server) serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error: " + err.Error()},
			})
			continue
		}

		result, rpcErr := s.dispatch(req.Method, req.Params)
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		_ = enc.Encode(resp)
	}
	return scanner.Err()
}

func (s *server) dispatch(method string, raw json.RawMessage) (interface{}, *rpcError) {
	switch method {
	case "initialize":
		return map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "xmanager", "version": config.Version},
		}, nil
	case "initialized":
		return struct{}{}, nil
	case "tools/list":
		return s.handleToolsList()
	case "tools/call":
		return s.handleToolsCall(raw)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

func (s *server) handleToolsList() (interface{}, *rpcError) {
	list := s.cat.List()
	tools := make([]map[string]interface{}, 0, len(list))
	for _, t := range list {
		tools = append(tools, map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return map[string]interface{}{"tools": tools}, nil
}

type callParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func (s *server) handleToolsCall(raw json.RawMessage) (interface{}, *rpcError) {
	var p callParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
	}
	if p.Arguments == nil {
		p.Arguments = map[string]interface{}{}
	}
	text, err := s.cat.Call(nil, p.Name, p.Arguments, ops.CallOptions{AllowDestructive: true})
	if err != nil {
		return callResult{
			Content: []textContent{{Type: "text", Text: fmt.Sprintf("error: %v", err)}},
			IsError: true,
		}, nil
	}
	return callResult{Content: []textContent{{Type: "text", Text: text}}}, nil
}
