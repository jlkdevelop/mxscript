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
	"sort"
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
				"signatureHelpProvider": map[string]any{
					"triggerCharacters":   []string{"(", ","},
					"retriggerCharacters": []string{","},
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
		// Lazy completion: every builtin name + every keyword + curated snippets.
		items := make([]map[string]any, 0, len(builtinDocs)+len(keywords)+len(snippets))
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
		for _, sn := range snippets {
			items = append(items, map[string]any{
				"label":            sn.Prefix,
				"kind":             15, // Snippet
				"detail":           sn.Description,
				"insertText":       sn.Body,
				"insertTextFormat": 2, // Snippet (with $1, $2 tabstops)
			})
		}
		return s.respond(req.ID, map[string]any{
			"isIncomplete": false,
			"items":        items,
		})

	case "textDocument/signatureHelp":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position struct {
				Line, Character int
			} `json:"position"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		text := s.getDoc(p.TextDocument.URI)
		fn, paramIdx := signatureContextAt(text, p.Position.Line, p.Position.Character)
		doc, ok := builtinDocs[fn]
		if !ok {
			return s.respond(req.ID, nil)
		}
		// Best-effort param parsing from the signature string:
		// "name(arg1, arg2?, ...args) -> ret" → ["arg1", "arg2?", "...args"]
		var paramLabels []map[string]any
		if open := strings.Index(doc.Signature, "("); open >= 0 {
			if close := strings.Index(doc.Signature[open:], ")"); close >= 0 {
				args := strings.TrimSpace(doc.Signature[open+1 : open+close])
				if args != "" {
					for _, a := range strings.Split(args, ",") {
						paramLabels = append(paramLabels, map[string]any{
							"label": strings.TrimSpace(a),
						})
					}
				}
			}
		}
		return s.respond(req.ID, map[string]any{
			"signatures": []map[string]any{{
				"label": doc.Signature,
				"documentation": map[string]string{
					"kind":  "markdown",
					"value": doc.Summary,
				},
				"parameters":      paramLabels,
				"activeParameter": paramIdx,
			}},
			"activeSignature": 0,
			"activeParameter": paramIdx,
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

// LookupDoc returns the signature and summary for a known builtin
// or namespace key (e.g. "json_stringify", "ai.complete"). Returns
// "", "", false when the name isn't in the curated docs table —
// callers can then decide whether to suggest something close or to
// print a generic "no docs" message.
func LookupDoc(name string) (sig, summary string, ok bool) {
	d, found := builtinDocs[name]
	if !found {
		return "", "", false
	}
	return d.Signature, d.Summary, true
}

// AllDocNames returns every name in the curated docs table, sorted.
// Used by `mx help` listing mode and `mx help <topic>`'s "did you
// mean" hint when the topic isn't found exactly.
func AllDocNames() []string {
	names := make([]string, 0, len(builtinDocs))
	for k := range builtinDocs {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

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
	"render":   {"render(path, vars?, partials?) -> response", "Render an HTML template file. Supports `{{ var }}`, `{{{ raw }}}`, `{{#if}}…{{else}}…{{/if}}`, `{{#each items}}…{{/each}}` (with `{{this}}`, `{{@index}}`), and `{{> partial}}`."},
	"render_string": {"render_string(tmpl, vars?, partials?) -> string", "Render an inline template. Same syntax as `render` — returns the result as a string."},

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
	"verify_webhook":           {"verify_webhook(secret, body, sig, scheme?) -> bool", "scheme: hex/base64/github/stripe."},
	"rate_limit":               {"rate_limit(key, max, window_seconds) -> bool", "Application-level token-bucket rate limit. Returns true if allowed."},
	"rate_limit_reset":         {"rate_limit_reset(key?)", "Reset one bucket (or all if key omitted). Test-only."},
	"ws.connect":              {"ws.connect(url, opts?) -> { send, recv, close }", "Outbound WebSocket client. Supports ws:// and wss://."},
	"cron":                     {"cron(spec, fn) -> stop_fn", "Vixie cron schedule. 5 fields: minute hour dom month dow. Returns a function that cancels the schedule."},
	"notify.slack":             {"notify.slack(webhook_url, message) -> { ok, status, error }", "Post to a Slack incoming webhook. Message can be a string or a full Slack payload object."},
	"notify.discord":           {"notify.discord(webhook_url, message) -> { ok, status, error }", "Post to a Discord webhook. String → content; object passes through (content, embeds, etc.)."},
	"notify.email":             {"notify.email(to, subject, body, opts?) -> { ok, status, error }", "Transactional email via Resend. Reads RESEND_API_KEY. opts: from, html, reply_to, cc, bcc."},
	"stripe.checkout":            {"stripe.checkout(price_id, opts?) -> { url, id }", "Create a Stripe Checkout Session. opts: mode, success_url, cancel_url, customer_email, customer, quantity."},
	"stripe.customer_create":     {"stripe.customer_create(email, opts?) -> { id, email }", "Create a Stripe Customer. opts: name, metadata."},
	"stripe.customer_portal":     {"stripe.customer_portal(customer_id, return_url) -> { url, id }", "Create a Customer Portal session URL."},
	"stripe.subscription_create": {"stripe.subscription_create(customer_id, price_id, opts?) -> { id, status }", "Start a subscription server-side. opts: trial_period_days."},
	"metrics.counter":            {"metrics.counter(name, value?, labels?)", "Increment a Prometheus counter (default 1). labels: object of string -> string."},
	"metrics.gauge":              {"metrics.gauge(name, value, labels?)", "Set a Prometheus gauge."},
	"metrics.histogram":          {"metrics.histogram(name, value, labels?)", "Record one observation in a histogram."},
	"metrics.handler":            {"metrics.handler() -> response", "Returns a Response for /metrics in Prometheus exposition format."},
	"metrics.text":               {"metrics.text() -> string", "Render the registry as Prometheus exposition text."},
	"metrics.reset":              {"metrics.reset()", "Clear every metric. Test-only."},
	"ai.complete":                {"ai.complete(prompt, opts?) -> string|object", "LLM completion across 10 providers. opts: provider, model, max_tokens, tools, messages."},
	"ai.stream":                  {"ai.stream(prompt, on_chunk, opts?) -> string", "Stream tokens to a callback. Same providers as ai.complete."},
	"ai.embed":                   {"ai.embed(text) -> array", "OpenAI text-embedding-3-small (vector of 1536 floats)."},
	"ai.similarity":              {"ai.similarity(a, b) -> number", "Cosine similarity between two embedding vectors."},
	"ai.vision":                  {"ai.vision(prompt, images, opts?) -> string", "Multimodal image+text. images: array of URLs or data: base64."},
	"ai.image":                   {"ai.image(prompt, opts?) -> { url, b64 }", "DALL-E image generation. opts: model, size, quality, format."},
	"ai.transcribe":              {"ai.transcribe(audio_path, opts?) -> string", "OpenAI Whisper speech-to-text. opts: model, language."},
	"jwt.sign":                   {"jwt.sign(claims, secret) -> string", "HS256-signed JWT."},
	"jwt.verify":                 {"jwt.verify(token, secret) -> object|null", "Returns claims, or null if signature/expiry invalid."},
	"sql.open":                   {"sql.open(dsn) -> handle", "Open SQLite/Postgres/MySQL by DSN scheme detection."},
	"sql.exec":                   {"sql.exec(db, query, ...args) -> { rows_affected, last_insert_id }", "INSERT / UPDATE / DELETE / DDL."},
	"sql.query":                  {"sql.query(db, query, ...args) -> array", "SELECT — array of objects keyed by column name."},
	"sql.query_one":              {"sql.query_one(db, query, ...args) -> object|null", "First row, or null."},
	"sql.transaction":            {"sql.transaction(db, fn(tx) { ... })", "Auto-commit on return; auto-rollback on throw."},
	"sql.migrate":                {"sql.migrate(db, [migrations]) -> { applied, skipped }", "Hash-tracked schema migrations."},
	"redis.connect":              {"redis.connect(dsn) -> handle", "Open a Redis connection (e.g. redis://localhost:6379/0)."},
	"redis.get":                  {"redis.get(r, key) -> string|null", ""},
	"redis.set":                  {"redis.set(r, key, value, opts?)", "opts.ttl_seconds for expiring keys."},
	"redis.incr":                 {"redis.incr(r, key) -> number", "Atomic increment by 1."},
	"redis.publish":              {"redis.publish(r, channel, message)", ""},
	"oauth.authorize_url":        {"oauth.authorize_url(opts) -> string", "Build the consent URL for one of: google, github, discord, linkedin, microsoft."},
	"oauth.exchange":             {"oauth.exchange(code, opts) -> object", "Trade an auth code for an access token."},
	"oauth.userinfo":             {"oauth.userinfo(token, opts) -> object", "Profile lookup at the provider's userinfo endpoint."},
	"password.hash":              {"password.hash(plain) -> string", "PBKDF2-SHA256 hash with salt baked in."},
	"password.verify":            {"password.verify(plain, hashed) -> bool", "Constant-time comparison."},
	"password.hash_argon2":       {"password.hash_argon2(plain) -> string", "Argon2id (RFC 9106) — recommended for new apps."},
	"password.verify_argon2":     {"password.verify_argon2(plain, hashed) -> bool", ""},
	"session.create":             {"session.create(secret, claims, opts?) -> response", "Set-Cookie with a signed session value. opts: max_age_seconds, http_only, secure, same_site."},
	"session.read":               {"session.read(request, secret) -> object|null", ""},
	"session.clear":              {"session.clear() -> response", "Set-Cookie with Max-Age=0 to remove the session."},
	"queue.enqueue":              {"queue.enqueue(payload, opts?)", "Enqueue a durable job. opts: delay_seconds."},
	"queue.process":              {"queue.process(workers?, fn(payload))", "Start N workers; fn runs per job."},
	"queue.close":                {"queue.close()", ""},
	"queue.stats":                {"queue.stats() -> object", "{ pending, running, done, failed } counts."},
	"pubsub.topic":                {"pubsub.topic() -> { subscribe, publish, count }", "In-process pub/sub channel."},
	"id.uuid":                    {"id.uuid() -> string", "RFC 4122 v4 UUID."},
	"id.ulid":                    {"id.ulid() -> string", "ULID — Crockford-base32, 26 chars, time-sortable."},
	"id.nanoid":                  {"id.nanoid(n?) -> string", "URL-safe random string. Default 21 chars."},
	"id.short":                   {"id.short() -> string", "8-char URL-safe ID. Good for invite codes."},
	"id.snowflake":               {"id.snowflake(epoch?) -> string", "64-bit time-sortable ID as a numeric string."},
	"pick":                       {"pick(obj, keys[]) -> object", "New object containing only the named keys."},
	"omit":                       {"omit(obj, keys[]) -> object", "New object with the named keys removed."},
	"merge":                      {"merge(a, b) -> object", "Shallow merge; b's values overwrite a's on key clash."},
	"pp":                         {"pp(value, opts?) -> value", "Pretty-print a value (indented, colored). Returns the value unchanged so it composes."},
	"deep_merge":                 {"deep_merge(a, b) -> object", "Recursive merge — descends into nested objects."},
	"search.create":              {"search.create(db, table, columns[])", "Create an FTS5 virtual table for full-text search."},
	"search.index":               {"search.index(db, table, id, doc)", "Insert (or replace) a row in the FTS5 table."},
	"search.query":               {"search.query(db, table, q, opts?) -> array", "Run an FTS5 MATCH query, BM25-ranked. opts: limit, offset."},
	"search.delete":              {"search.delete(db, table, id)", "Delete a row from the FTS5 table by id."},
	"s3.put":                     {"s3.put(bucket, key, body, opts?)", "Upload an object. opts: endpoint, region, content_type, access_key, secret_key."},
	"s3.get":                     {"s3.get(bucket, key, opts?) -> string", "Download an object's body."},
	"s3.delete":                  {"s3.delete(bucket, key, opts?)", "Delete an object."},
	"s3.list":                    {"s3.list(bucket, prefix?, opts?) -> array", "List up to 1000 keys (optionally filtered by prefix)."},
	"s3.presign":                 {"s3.presign(bucket, key, opts?) -> string", "Presigned GET URL. opts: expires (seconds, default 3600)."},
	"image.thumbnail":            {"image.thumbnail(bytes, max_size, opts?) -> bytes", "Fit within max_size × max_size, preserving aspect ratio."},
	"image.crop":                 {"image.crop(bytes, x, y, w, h, opts?) -> bytes", "Extract a rectangular region (top-left origin)."},
	"magic_link.create":        {"magic_link.create(email, secret, opts?) -> string", "Signed time-limited token for passwordless email login. opts.expires_minutes (default 15)."},
	"magic_link.verify":        {"magic_link.verify(token, secret) -> string|null", "Returns the email if the token is valid and unexpired, otherwise null."},
	"totp.generate":            {"totp.generate(secret) -> string", "Current 6-digit RFC 6238 code. Secret is base32 (case- and padding-tolerant)."},
	"totp.verify":              {"totp.verify(code, secret, drift?) -> bool", "Accepts ±drift × 30s slots (default 1 = ±30s window)."},
	"totp.uri":                 {"totp.uri(account, secret, issuer?) -> string", "otpauth:// URI suitable for QR encoding. Scan with Google Authenticator / Authy / 1Password."},
	"webhooks.verify_stripe":   {"webhooks.verify_stripe(payload, signature_header, secret, tolerance?) -> bool", "Stripe-Signature with timestamp tolerance (default 300s)."},
	"webhooks.verify_github":   {"webhooks.verify_github(payload, signature, secret) -> bool", "X-Hub-Signature-256 = sha256=<hex>."},
	"webhooks.verify_svix":     {"webhooks.verify_svix(payload, msg_id, timestamp, signature, secret) -> bool", "Svix / Resend / Clerk / Discord."},
	"webhooks.verify_shopify":  {"webhooks.verify_shopify(payload, signature, secret) -> bool", "X-Shopify-Hmac-Sha256 = base64."},
	"webhooks.verify_slack":    {"webhooks.verify_slack(payload, timestamp, signature, secret, tolerance?) -> bool", "Slack v0=<hex>, with timestamp guard."},

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

// snippets are common-pattern completions. Editors render them with
// $1 / $2 / $0 as tab stops the way VS Code / Helix / Zed expect.
type snippet struct {
	Prefix      string
	Description string
	Body        string
}

var snippets = []snippet{
	{Prefix: "route", Description: "GET route", Body: "get ${1:/path} {\n\treturn json(${2:{}})\n}\n"},
	{Prefix: "post", Description: "POST route with validation", Body: "post ${1:/path} {\n\tlet r = validate(request.body, {\n\t\ttype: \"object\",\n\t\trequired: [${2:\"name\"}],\n\t\tproperties: {\n\t\t\t${3}\n\t\t}\n\t})\n\tif (!r.valid) { return status(400, { errors: r.errors }) }\n\t${0}\n}\n"},
	{Prefix: "group", Description: "Route group with auth", Body: "group ${1:/api/v1} {\n\tuse ${2:require_auth}\n\n\t${0}\n}\n"},
	{Prefix: "mw", Description: "middleware definition", Body: "middleware ${1:require_auth} {\n\tlet claims = jwt.verify(request.bearer_token, env_required(\"JWT_SECRET\"))\n\tif (claims == null) {\n\t\treturn status(401, { error: \"unauthorized\" })\n\t}\n}\n"},
	{Prefix: "ws", Description: "WebSocket route", Body: "ws ${1:/chat} {\n\twhile (true) {\n\t\tlet msg = recv()\n\t\tif (msg == null) { break }\n\t\t${0:send(\"echo: \" + msg)}\n\t}\n}\n"},
	{Prefix: "sse", Description: "SSE route", Body: "sse ${1:/events} {\n\twhile (true) {\n\t\tsend({ ${2:tick: now()} })\n\t\tsleep(${3:1000})\n\t}\n}\n"},
	{Prefix: "fn", Description: "function declaration", Body: "fn ${1:name}(${2:args}) {\n\t${0}\n}\n"},
	{Prefix: "match", Description: "match expression", Body: "match ${1:value} {\n\t${2:1} => ${3:\"one\"}\n\t${4:_} => ${5:\"other\"}\n}\n"},
	{Prefix: "try", Description: "try/catch block", Body: "try {\n\t${1}\n} catch (e) {\n\t${0:return status(500, { error: e.message })}\n}\n"},
	{Prefix: "spawn", Description: "spawn block", Body: "spawn {\n\t${0}\n}\n"},
	{Prefix: "test", Description: "test_* function", Body: "fn test_${1:name}() {\n\tassert_eq(${2:actual}, ${3:expected})\n}\n"},
	{Prefix: "bench", Description: "bench_* function", Body: "fn bench_${1:name}() {\n\t${0}\n}\n"},
	{Prefix: "server", Description: "server config block", Body: "server {\n\tport: ${1:8080},\n\tlog: ${2:true},\n\tcors: { origins: [\"*\"] }\n}\n"},
	{Prefix: "sql.migrate", Description: "schema migrations", Body: "sql.migrate(db, [\n\t\"CREATE TABLE IF NOT EXISTS ${1:users} (id INTEGER PRIMARY KEY, ${2:name TEXT})\"\n])\n"},
	{Prefix: "session", Description: "session.create / read", Body: "post /login {\n\treturn session.create({ user_id: ${1:user.id} }, { secret: env_required(\"SESSION_SECRET\") })\n}\n\nget /me {\n\tlet claims = session.read(request, env(\"SESSION_SECRET\"))\n\tif (claims == null) { return status(401) }\n\treturn json(claims)\n}\n"},
	{Prefix: "openapi", Description: "OpenAPI + Swagger UI", Body: "get /openapi.json { return json(openapi({ title: \"${1:My API}\" })) }\nget /docs        { return swagger_ui(\"/openapi.json\") }\n"},
}

// signatureContextAt walks back from the cursor to find the enclosing
// `name(...` call, returning the function name and the active parameter
// index (0-based, counted by commas at the same nesting depth).
func signatureContextAt(text string, line, col int) (string, int) {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return "", 0
	}
	row := lines[line]
	if col > len(row) {
		col = len(row)
	}
	depth := 0
	commaCount := 0
	for i := col - 1; i >= 0; i-- {
		c := row[i]
		switch {
		case c == ')' || c == ']' || c == '}':
			depth++
		case c == '(':
			if depth == 0 {
				// Walk back to grab the identifier just before the paren.
				name := identBefore(row, i)
				return name, commaCount
			}
			depth--
		case c == '[' || c == '{':
			depth--
		case c == ',' && depth == 0:
			commaCount++
		}
	}
	return "", 0
}

func identBefore(s string, end int) string {
	start := end
	for start > 0 {
		c := s[start-1]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' {
			start--
			continue
		}
		break
	}
	return s[start:end]
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
