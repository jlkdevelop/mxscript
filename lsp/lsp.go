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
				"completionProvider": map[string]any{
					"triggerCharacters": []string{".", " "},
				},
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
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"position"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		text := s.getDoc(p.TextDocument.URI)
		word := wordAt(text, p.Position.Line, p.Position.Character)
		if doc, ok := builtinDocs[word]; ok {
			return s.respond(req.ID, map[string]any{
				"contents": map[string]string{
					"kind":  "markdown",
					"value": "**" + word + "**\n\n```mx\n" + doc.Signature + "\n```\n\n" + doc.Summary,
				},
			})
		}
		return s.respond(req.ID, nil)

	case "textDocument/completion":
		// Lazy completion: every builtin name + every keyword gets offered.
		// Editors filter by prefix client-side, so we don't have to.
		items := make([]map[string]any, 0, len(builtinDocs)+len(keywords))
		for name, doc := range builtinDocs {
			items = append(items, map[string]any{
				"label":  name,
				"kind":   3, // Function
				"detail": doc.Signature,
				"documentation": map[string]string{
					"kind":  "markdown",
					"value": doc.Summary,
				},
			})
		}
		for _, kw := range keywords {
			items = append(items, map[string]any{
				"label": kw,
				"kind":  14, // Keyword
			})
		}
		return s.respond(req.ID, map[string]any{
			"isIncomplete": false,
			"items":        items,
		})
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

// ===== Builtin signatures (for hover + completion) =====

type builtinDoc struct {
	Signature string
	Summary   string
}

// Hand-authored signatures for the most useful builtins. Not auto-generated
// from the interpreter — keeping this curated keeps the summaries readable.
var builtinDocs = map[string]builtinDoc{
	// Output
	"print":   {"print(...args)", "Print arguments to stdout, space-separated, with a trailing newline."},
	"println": {"println(...args)", "Alias for `print`."},
	"write":   {"write(...args)", "Print without trailing newline."},
	"eprint":  {"eprint(...args)", "Print to stderr."},
	"format":  {"format(fmt, ...args) -> string", "Printf-style formatter (%s %d %f %v)."},

	// HTTP responses
	"json":     {"json(value, opts?) -> response", "JSON response. opts may include cookies / headers."},
	"text":     {"text(value, opts?) -> response", "Plain text response."},
	"html":     {"html(value, opts?) -> response", "HTML response."},
	"status":   {"status(code, body?, opts?) -> response", "Custom status code."},
	"redirect": {"redirect(url, code?) -> response", "302 redirect (or supplied code)."},
	"render":   {"render(path, vars?) -> response", "Render an HTML template file with `{{ vars }}`."},

	// HTTP request
	"fetch": {"fetch(url, opts?) -> { status, headers, body, text }", "Outbound HTTP request."},

	// Env
	"env":          {"env(name, default?) -> string", "Read an environment variable."},
	"env_required": {"env_required(name) -> string", "Like env, but throws if unset."},

	// Strings
	"upper":       {"upper(s) -> string", "Uppercase."},
	"lower":       {"lower(s) -> string", "Lowercase."},
	"trim":        {"trim(s) -> string", "Strip leading/trailing whitespace."},
	"split":       {"split(s, sep) -> array", "Split into array."},
	"contains":    {"contains(haystack, needle) -> bool", "Substring (or array membership) check."},
	"replace":     {"replace(s, old, new) -> string", "Replace all occurrences."},
	"starts_with": {"starts_with(s, prefix) -> bool", "Prefix check."},
	"ends_with":   {"ends_with(s, suffix) -> bool", "Suffix check."},
	"pad_left":    {"pad_left(s, w, ch?) -> string", "Pad left to width."},
	"pad_right":   {"pad_right(s, w, ch?) -> string", "Pad right to width."},
	"repeat":      {"repeat(s, n) -> string", "Repeat string n times."},
	"substr":      {"substr(s, start, len?) -> string", "Slice. Negative start counts from end."},
	"index_of":    {"index_of(s, sub) -> number", "Rune index of first match, or -1."},
	"slug":        {"slug(s) -> string", "URL-safe lowercase identifier."},
	"html_escape": {"html_escape(s) -> string", "Escape HTML special chars."},
	"markdown":    {"markdown(s) -> string", "Render CommonMark subset to safe HTML."},

	// Arrays
	"len":      {"len(x) -> number", "Length of string / array / object."},
	"push":     {"push(arr, ...vals) -> array", "Append values, returning a new array."},
	"pop":      {"pop(arr) -> any", "Last element, or null."},
	"map":      {"map(arr, fn) -> array", "Transform each element."},
	"filter":   {"filter(arr, fn) -> array", "Keep elements where fn returns truthy."},
	"find":     {"find(arr, fn) -> any", "First matching element, or null."},
	"reduce":   {"reduce(arr, fn, init) -> any", "Fold to single value."},
	"sort":     {"sort(arr) -> array", "Ascending sort (numbers / strings)."},
	"sort_by":  {"sort_by(arr, key_fn) -> array", "Sort with a key extractor."},
	"sum":      {"sum(arr) -> number", "Sum a numeric array."},
	"group_by": {"group_by(arr, key_fn) -> object", "Partition into key -> [items]."},
	"unique":   {"unique(arr) -> array", "Dedupe, first occurrence wins."},
	"flatten":  {"flatten(arr) -> array", "One level of nesting."},
	"zip":      {"zip(a, b) -> array", "Pairs of [a[i], b[i]]."},
	"range":    {"range(end) | range(start, end) -> array", "Numeric range."},
	"join":     {"join(arr, sep?) -> string", "Concatenate with separator."},

	// Math
	"round": {"round(n) -> number", "Round to nearest integer."},
	"floor": {"floor(n) -> number", "Round toward -∞."},
	"ceil":  {"ceil(n) -> number", "Round toward +∞."},
	"abs":   {"abs(n) -> number", "Absolute value."},
	"min":   {"min(...nums) -> number", "Smallest."},
	"max":   {"max(...nums) -> number", "Largest."},
	"pow":   {"pow(base, exp) -> number", "Exponentiation."},
	"sqrt":  {"sqrt(n) -> number", "Square root."},

	// Types
	"typeof":     {"typeof(x) -> string", `"null"|"bool"|"number"|"string"|"array"|"object"|"function"|"channel"|"handle"`},
	"isString":   {"isString(x) -> bool", ""},
	"isNumber":   {"isNumber(x) -> bool", ""},
	"isBool":     {"isBool(x) -> bool", ""},
	"isNull":     {"isNull(x) -> bool", ""},
	"isArray":    {"isArray(x) -> bool", ""},
	"isObject":   {"isObject(x) -> bool", ""},
	"isFunction": {"isFunction(x) -> bool", ""},

	// JSON
	"json_parse":     {"json_parse(s) -> any", "Parse JSON string."},
	"json_stringify": {"json_stringify(v, pretty?) -> string", "Serialize to JSON."},

	// Time
	"now":          {"now() -> number", "Current Unix time in milliseconds."},
	"now_iso":      {"now_iso() -> string", "Current UTC time as RFC 3339."},
	"sleep":        {"sleep(ms)", "Block for ms milliseconds."},
	"parse_date":   {"parse_date(s, layout?) -> number", "Parse to Unix ms."},
	"format_date":  {"format_date(ms, layout?) -> string", "Format Unix ms."},
	"add_days":     {"add_days(ms, n) -> number", ""},
	"add_hours":    {"add_hours(ms, n) -> number", ""},
	"add_minutes":  {"add_minutes(ms, n) -> number", ""},
	"days_between": {"days_between(a, b) -> number", ""},
	"weekday":      {"weekday(ms) -> string", ""},
	"time_ago":     {"time_ago(ms) -> string", "'5 minutes ago' / 'in 30 seconds' / 'just now'"},
	"time_human":   {"time_human(ms) -> string", "Locale-formatted human-readable."},

	// IDs / crypto
	"uuid":           {"uuid() -> string", "RFC 4122 v4 UUID."},
	"hash_sha256":    {"hash_sha256(s) -> string", "SHA-256 hex digest."},
	"hmac_sha256":    {"hmac_sha256(secret, msg) -> string", "HMAC-SHA-256 hex."},
	"base64_encode":  {"base64_encode(s) -> string", ""},
	"base64_decode":  {"base64_decode(s) -> string", ""},
	"aes_encrypt":    {"aes_encrypt(plaintext, key) -> string", "AES-256-GCM. Output is base64."},
	"aes_decrypt":    {"aes_decrypt(ciphertext, key) -> string", "Inverse of aes_encrypt."},
	"sign_cookie":    {"sign_cookie(secret, value) -> string", "Tamper-evident signed cookie value."},
	"verify_cookie":  {"verify_cookie(secret, signed) -> string|null", ""},
	"verify_webhook": {"verify_webhook(secret, body, sig, scheme?) -> bool", "scheme: hex/base64/github/stripe."},

	// Regex
	"re_match":    {"re_match(pattern, s) -> bool", ""},
	"re_find":     {"re_find(pattern, s) -> string|array|null", ""},
	"re_find_all": {"re_find_all(pattern, s) -> array", ""},
	"re_replace":  {"re_replace(pattern, s, repl) -> string", ""},

	// URL
	"parse_url":  {"parse_url(s) -> object", "{ scheme, host, port, path, query, fragment, raw }"},
	"url_encode": {"url_encode(s) -> string", ""},
	"url_decode": {"url_decode(s) -> string", ""},

	// File I/O
	"read_file":   {"read_file(path) -> string", ""},
	"write_file":  {"write_file(path, content)", ""},
	"file_exists": {"file_exists(path) -> bool", ""},
	"list_files":  {"list_files(dir) -> array", ""},
	"delete_file": {"delete_file(path)", ""},
	"shell":       {"shell(cmd, args?, opts?) -> { stdout, stderr, exit_code }", "Run an OS command."},

	// KV store
	"kv_get":    {"kv_get(path, key) -> any", ""},
	"kv_set":    {"kv_set(path, key, value)", ""},
	"kv_delete": {"kv_delete(path, key) -> bool", ""},
	"kv_keys":   {"kv_keys(path) -> array", ""},
	"kv_clear":  {"kv_clear(path)", ""},

	// CSV
	"csv_parse":     {"csv_parse(text) -> array of arrays", ""},
	"csv_stringify": {"csv_stringify(rows) -> string", ""},

	// Concurrency
	"chan":       {"chan(capacity?) -> channel", "Allocate a channel (0 = unbuffered)."},
	"send":       {"send(ch, value)", "Send on a channel (blocks if full)."},
	"recv":       {"recv(ch) -> any", "Receive (returns null when closed)."},
	"close_chan": {"close_chan(ch)", "Close a channel."},
	"wait_group": {"wait_group() -> { add, done, wait }", "sync.WaitGroup wrapper."},
	"every":      {"every(duration, fn) -> stop_fn", "Run fn periodically."},
	"after":      {"after(duration, fn) -> cancel_fn", "Run fn once after delay."},
	"debounce":   {"debounce(duration, fn) -> wrapper", ""},

	// Test
	"assert":    {"assert(cond, msg?)", ""},
	"assert_eq": {"assert_eq(a, b, msg?)", ""},

	// Misc
	"retry": {"retry(fn, attempts, delay_ms?) -> any", "Call fn up to attempts times until non-error."},
	"error": {"error(msg)", "Throw a runtime error (catchable with try/catch)."},
}

// keywords are offered as completions too, alongside builtins.
var keywords = []string{
	"let", "fn", "return", "if", "else", "loop", "as", "while", "break", "continue",
	"route", "server", "middleware", "use", "static", "import", "export",
	"try", "catch", "match", "spawn", "true", "false", "null",
	"get", "post", "put", "delete", "patch", "head", "options", "sse", "ws",
}

// wordAt extracts the identifier under the cursor at line/col. Returns ""
// if the position isn't on an identifier.
func wordAt(text string, line, col int) string {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	row := lines[line]
	if col < 0 || col > len(row) {
		return ""
	}
	isIdent := func(b byte) bool {
		return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
	}
	start := col
	for start > 0 && isIdent(row[start-1]) {
		start--
	}
	end := col
	for end < len(row) && isIdent(row[end]) {
		end++
	}
	if start == end {
		return ""
	}
	return row[start:end]
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
