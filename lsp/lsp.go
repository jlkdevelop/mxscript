// Package lsp implements a tiny Language Server Protocol server for
// MX Script. It speaks JSON-RPC 2.0 over stdio and exposes:
//
//   - textDocument/didOpen, didChange, didClose, didSave — track buffers
//   - textDocument/publishDiagnostics — parse errors as squiggles
//   - textDocument/formatting — invoke mx fmt
//   - textDocument/hover — minimal "did you mean ___" for builtins
//
// Usage from VS Code (or any LSP client):
//
//	{
//	  "command": "mx",
//	  "args": ["lsp"],
//	  "filetypes": ["mxscript"]
//	}
package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/jlkdevelop/mxscript/formatter"
	"github.com/jlkdevelop/mxscript/lexer"
	"github.com/jlkdevelop/mxscript/parser"
)

// Run reads JSON-RPC messages from r, replies on w, and only returns when
// the client closes stdin (or sends `shutdown` + `exit`).
func Run(r io.Reader, w io.Writer) error {
	srv := &server{
		w:    w,
		docs: map[string]string{},
	}
	srv.encoder = json.NewEncoder(w)
	br := bufio.NewReader(r)
	for {
		body, err := readMessage(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := srv.handle(body); err != nil {
			return err
		}
	}
}

// ===== Wire protocol =====

func readMessage(r *bufio.Reader) ([]byte, error) {
	var contentLen int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[len("Content-Length:"):]))
			if err != nil {
				return nil, err
			}
			contentLen = n
		}
	}
	if contentLen == 0 {
		return nil, errors.New("missing Content-Length")
	}
	body := make([]byte, contentLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// ===== Server =====

type server struct {
	w       io.Writer
	encoder *json.Encoder
	mu      sync.Mutex // protects docs + writer
	docs    map[string]string
}

type rpcMessage struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *server) handle(body []byte) error {
	var req rpcMessage
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}

	switch req.Method {
	case "initialize":
		return s.respond(req.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":           1, // full sync
				"documentFormattingProvider": true,
				"hoverProvider":              true,
			},
			"serverInfo": map[string]string{
				"name":    "mx-lsp",
				"version": "1.0",
			},
		})
	case "initialized":
		return nil
	case "shutdown":
		return s.respond(req.ID, nil)
	case "exit":
		// graceful exit
		return io.EOF

	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		s.setDoc(p.TextDocument.URI, p.TextDocument.Text)
		return s.publishDiagnostics(p.TextDocument.URI, p.TextDocument.Text)

	case "textDocument/didChange":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		if len(p.ContentChanges) > 0 {
			text := p.ContentChanges[0].Text
			s.setDoc(p.TextDocument.URI, text)
			return s.publishDiagnostics(p.TextDocument.URI, text)
		}
		return nil

	case "textDocument/didClose":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		s.deleteDoc(p.TextDocument.URI)
		return nil

	case "textDocument/formatting":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		text := s.getDoc(p.TextDocument.URI)
		out, err := formatter.Format(text)
		if err != nil {
			return s.respondError(req.ID, -32603, err.Error())
		}
		if out == text {
			return s.respond(req.ID, []any{})
		}
		// Replace the entire document.
		end := lineColAfter(text)
		return s.respond(req.ID, []any{
			map[string]any{
				"range": map[string]any{
					"start": map[string]int{"line": 0, "character": 0},
					"end":   map[string]any{"line": end.Line, "character": end.Col},
				},
				"newText": out,
			},
		})

	case "textDocument/hover":
		// Stub: return nothing; clients won't error on null.
		return s.respond(req.ID, nil)
	}

	// Unknown method — ignore (notifications) or respond with method-not-found.
	if len(req.ID) > 0 {
		return s.respondError(req.ID, -32601, "method not found: "+req.Method)
	}
	return nil
}

func (s *server) setDoc(uri, text string) {
	s.mu.Lock()
	s.docs[uri] = text
	s.mu.Unlock()
}

func (s *server) getDoc(uri string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.docs[uri]
}

func (s *server) deleteDoc(uri string) {
	s.mu.Lock()
	delete(s.docs, uri)
	s.mu.Unlock()
}

// publishDiagnostics lexes + parses the document and sends any errors as
// LSP diagnostics. An empty diagnostics array clears stale squiggles.
func (s *server) publishDiagnostics(uri, text string) error {
	var diags []map[string]any

	tokens, err := lexer.New(text).Tokenize()
	if err != nil {
		// Lexer errors don't carry structured line/col so report at 0:0.
		diags = append(diags, diagnosticFor(0, 0, err.Error()))
	} else if _, err := parser.New(tokens).Parse(); err != nil {
		var pe *parser.ParseError
		if errors.As(err, &pe) {
			diags = append(diags, diagnosticFor(pe.Line-1, pe.Col-1, pe.Message))
		} else {
			diags = append(diags, diagnosticFor(0, 0, err.Error()))
		}
	}

	return s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         uri,
		"diagnostics": diags,
	})
}

func diagnosticFor(line, col int, msg string) map[string]any {
	return map[string]any{
		"range": map[string]any{
			"start": map[string]int{"line": line, "character": col},
			"end":   map[string]int{"line": line, "character": col + 1},
		},
		"severity": 1, // Error
		"source":   "mxscript",
		"message":  msg,
	}
}

// ===== Helpers =====

type pos struct{ Line, Col int }

func lineColAfter(text string) pos {
	line := strings.Count(text, "\n")
	col := 0
	if i := strings.LastIndex(text, "\n"); i >= 0 {
		col = len(text) - i - 1
	} else {
		col = len(text)
	}
	return pos{Line: line, Col: col}
}

func (s *server) respond(id json.RawMessage, result any) error {
	return s.write(rpcMessage{Jsonrpc: "2.0", ID: id, Result: result})
}

func (s *server) respondError(id json.RawMessage, code int, msg string) error {
	return s.write(rpcMessage{Jsonrpc: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *server) notify(method string, params any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return s.write(rpcMessage{Jsonrpc: "2.0", Method: method, Params: body})
}

func (s *server) write(msg rpcMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write([]byte(header)); err != nil {
		return err
	}
	_, err = s.w.Write(body)
	return err
}
