// Package interpreter is the heart of MX Script. It walks the parsed AST,
// evaluates expressions, drives the standard library, and (when route
// declarations are present) starts an HTTP server that dispatches incoming
// requests to user-defined route bodies.
package interpreter

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jlkdevelop/mxscript/parser"
)

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// clientIP extracts the originating client IP, honoring the standard
// proxy / load-balancer forwarding headers. Falls back to RemoteAddr.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		// Comma-separated; first entry is the original client.
		if i := strings.Index(v, ","); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	addr := r.RemoteAddr
	// Strip the trailing :port if present.
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		addr = addr[:i]
	}
	return addr
}

// parseMultipart pulls fields and files out of a multipart/form-data
// request. Each entry in `files` is { name, content_type, size, content }
// where content is the raw bytes as a string. Entries in `fields` are
// plain strings (matching the form-urlencoded shape).
func parseMultipart(r *http.Request) (*OrderedMap, *OrderedMap, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32 MiB in-memory cap
		return nil, nil, err
	}
	fields := NewOrderedMap()
	files := NewOrderedMap()
	if r.MultipartForm == nil {
		return fields, files, nil
	}
	// Plain text fields.
	keys := make([]string, 0, len(r.MultipartForm.Value))
	for k := range r.MultipartForm.Value {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := r.MultipartForm.Value[k]
		if len(v) > 0 {
			fields.Set(k, StringValue(v[0]))
		}
	}
	// File uploads.
	fkeys := make([]string, 0, len(r.MultipartForm.File))
	for k := range r.MultipartForm.File {
		fkeys = append(fkeys, k)
	}
	sort.Strings(fkeys)
	for _, k := range fkeys {
		fhs := r.MultipartForm.File[k]
		if len(fhs) == 0 {
			continue
		}
		fh := fhs[0]
		f, err := fh.Open()
		if err != nil {
			return nil, nil, err
		}
		body, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, nil, err
		}
		obj := NewOrderedMap()
		obj.Set("name", StringValue(fh.Filename))
		obj.Set("size", NumberValue(float64(fh.Size)))
		obj.Set("content_type", StringValue(fh.Header.Get("Content-Type")))
		obj.Set("content", StringValue(string(body)))
		files.Set(k, ObjectValue(obj))
	}
	return fields, files, nil
}

// gzipWriter wraps an http.ResponseWriter so writes go through a gzip stream.
type gzipWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipWriter) Write(b []byte) (int, error) { return g.gz.Write(b) }

// statusRecorder captures the response status for the access logger.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// suggestIdentifier walks the env chain looking for a known name within
// edit-distance 2 of `name`. Returns the closest match (or "" if none).
// Used to power the "did you mean ___" hint on undefined-identifier errors.
func suggestIdentifier(env *Env, name string) string {
	candidates := allKnownIdents(env)
	best := ""
	bestDist := 3 // require <= 2 to suggest
	lower := strings.ToLower(name)
	for _, c := range candidates {
		d := levenshtein(lower, strings.ToLower(c))
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best
}

func allKnownIdents(env *Env) []string {
	seen := map[string]bool{}
	var out []string
	for e := env; e != nil; e = e.parent {
		for _, k := range e.Keys() {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}

// levenshtein is a tiny DP edit-distance — fast enough for ~200 ident
// candidates per error. We don't bother caching since errors are rare.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = prev[j] + 1
			if curr[j-1]+1 < curr[j] {
				curr[j] = curr[j-1] + 1
			}
			if prev[j-1]+cost < curr[j] {
				curr[j] = prev[j-1] + cost
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// pickAllowedOrigin returns the first matching allowed origin (or "*" if
// the wildcard is configured). Empty result means the request origin is
// not allowed; the caller omits the Access-Control-Allow-Origin header.
func pickAllowedOrigin(allowed []string, origin string) string {
	if origin == "" {
		// Server-to-server / curl — still echo the wildcard if configured.
		for _, a := range allowed {
			if a == "*" {
				return "*"
			}
		}
		return ""
	}
	for _, a := range allowed {
		if a == "*" {
			return "*"
		}
		if a == origin {
			return origin
		}
	}
	return ""
}

// ===== Runtime values =====

type ValueKind int

const (
	KindNull ValueKind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
	KindFunction
	KindResponse
	KindChannel
	KindHandle
)

// Channel wraps a Go channel of MX values. Allocated by chan(); operated
// on with send(ch, v), recv(ch), close_chan(ch).
type Channel struct {
	C    chan Value
	once sync.Once
}

func (c *Channel) Close() { c.once.Do(func() { close(c.C) }) }

type Value struct {
	Kind     ValueKind
	Bool     bool
	Number   float64
	String   string
	Array    []Value
	Object   *OrderedMap
	Function *Function
	Response *Response
	Channel  *Channel
	// Handle is a generic opaque pointer for resources that don't fit
	// the standard value shapes — DB connections, future file handles,
	// etc. Treated as a black box by the interpreter.
	Handle any
}

func HandleValue(h any) Value { return Value{Kind: KindHandle, Handle: h} }

func ChannelValue(c *Channel) Value { return Value{Kind: KindChannel, Channel: c} }

type Function struct {
	Name    string
	Params  []string
	Body    []parser.Stmt
	Closure *Env
	Native  func(interp *Interpreter, args []Value) (Value, error)
}

type Response struct {
	Status      int
	ContentType string
	Body        Value
	Headers     map[string]string
	Cookies     []*http.Cookie
}

// OrderedMap preserves insertion order of object keys for predictable JSON output.
type OrderedMap struct {
	Keys   []string
	Values map[string]Value
}

func NewOrderedMap() *OrderedMap {
	return &OrderedMap{Values: map[string]Value{}}
}

func (o *OrderedMap) Set(k string, v Value) {
	if _, exists := o.Values[k]; !exists {
		o.Keys = append(o.Keys, k)
	}
	o.Values[k] = v
}

func (o *OrderedMap) Get(k string) (Value, bool) {
	v, ok := o.Values[k]
	return v, ok
}

// Helpers to construct values.
func NullValue() Value                { return Value{Kind: KindNull} }
func BoolValue(b bool) Value          { return Value{Kind: KindBool, Bool: b} }
func NumberValue(n float64) Value     { return Value{Kind: KindNumber, Number: n} }
func StringValue(s string) Value      { return Value{Kind: KindString, String: s} }
func ArrayValue(a []Value) Value      { return Value{Kind: KindArray, Array: a} }
func ObjectValue(o *OrderedMap) Value { return Value{Kind: KindObject, Object: o} }
func FunctionValue(f *Function) Value { return Value{Kind: KindFunction, Function: f} }
func ResponseValue(r *Response) Value { return Value{Kind: KindResponse, Response: r} }

// IsTruthy follows the rules: null/false/0/""/[]/{} are falsy; anything else is truthy.
func (v Value) IsTruthy() bool {
	switch v.Kind {
	case KindNull:
		return false
	case KindBool:
		return v.Bool
	case KindNumber:
		return v.Number != 0
	case KindString:
		return v.String != ""
	case KindArray:
		return len(v.Array) > 0
	case KindObject:
		return v.Object != nil && len(v.Object.Keys) > 0
	}
	return true
}

func (v Value) typeName() string {
	switch v.Kind {
	case KindNull:
		return "null"
	case KindBool:
		return "bool"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindArray:
		return "array"
	case KindObject:
		return "object"
	case KindFunction:
		return "function"
	case KindResponse:
		return "response"
	case KindChannel:
		return "channel"
	case KindHandle:
		return "handle"
	}
	return "unknown"
}

// String produces a human-readable representation, used by print().
func (v Value) Display() string {
	switch v.Kind {
	case KindNull:
		return "null"
	case KindBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case KindNumber:
		if v.Number == math.Trunc(v.Number) && !math.IsInf(v.Number, 0) {
			return strconv.FormatInt(int64(v.Number), 10)
		}
		return strconv.FormatFloat(v.Number, 'g', -1, 64)
	case KindString:
		return v.String
	case KindArray, KindObject, KindResponse:
		b, _ := jsonEncode(v)
		return string(b)
	case KindFunction:
		if v.Function.Name != "" {
			return "<fn " + v.Function.Name + ">"
		}
		return "<fn>"
	case KindChannel:
		return "<channel>"
	case KindHandle:
		return "<handle>"
	}
	return ""
}

// ===== Environment =====

// Env is the lexical scope. RWMutex protects the vars map so spawned
// goroutines can read/write the closure safely. (Programs that share
// MUTABLE state across spawns still race on the values themselves —
// use channels for coordination.)
type Env struct {
	parent *Env
	mu     sync.RWMutex
	vars   map[string]Value
}

func NewEnv(parent *Env) *Env { return &Env{parent: parent, vars: map[string]Value{}} }

func (e *Env) Get(name string) (Value, bool) {
	e.mu.RLock()
	v, ok := e.vars[name]
	e.mu.RUnlock()
	if ok {
		return v, true
	}
	if e.parent != nil {
		return e.parent.Get(name)
	}
	return Value{}, false
}

func (e *Env) Set(name string, v Value) {
	e.mu.Lock()
	e.vars[name] = v
	e.mu.Unlock()
}

// Keys returns the names defined directly in this scope (not parents).
// Used by the REPL to show what the user has bound.
func (e *Env) Keys() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	keys := make([]string, 0, len(e.vars))
	for k := range e.vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Assign walks up parents until the variable is found, then replaces it.
// If not found, defines it in the current scope.
func (e *Env) Assign(name string, v Value) {
	e.mu.Lock()
	if _, ok := e.vars[name]; ok {
		e.vars[name] = v
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()
	if e.parent != nil {
		if _, ok := e.parent.lookup(name); ok {
			e.parent.Assign(name, v)
			return
		}
	}
	e.mu.Lock()
	e.vars[name] = v
	e.mu.Unlock()
}

func (e *Env) lookup(name string) (Value, bool) {
	e.mu.RLock()
	v, ok := e.vars[name]
	e.mu.RUnlock()
	if ok {
		return v, true
	}
	if e.parent != nil {
		return e.parent.lookup(name)
	}
	return Value{}, false
}

// ===== Control-flow signals =====

type returnSignal struct{ Value Value }

func (r *returnSignal) Error() string { return "return" }

type breakSignal struct{}

func (*breakSignal) Error() string { return "break" }

type continueSignal struct{}

func (*continueSignal) Error() string { return "continue" }

// MXError is a structured runtime error with file/line context and
// the active call stack at the moment of failure.
type MXError struct {
	Message string
	Line    int
	Col     int
	File    string
	Stack   []StackFrame
}

// StackFrame describes one active call when an error fired.
type StackFrame struct {
	Name string
	Line int
	Col  int
}

func (e *MXError) Error() string {
	loc := ""
	if e.File != "" {
		loc = e.File + ":"
	}
	if e.Line > 0 {
		return fmt.Sprintf("%s%d:%d: %s", loc, e.Line, e.Col, e.Message)
	}
	return e.Message
}

func runtimeErrorf(node parser.Node, format string, args ...any) *MXError {
	line, col := node.Pos()
	return &MXError{Message: fmt.Sprintf(format, args...), Line: line, Col: col}
}

// ===== Interpreter =====

type registeredRoute struct {
	Method      string
	PathParts   []string
	Body        []parser.Stmt
	Middlewares []string
}

type staticMount struct {
	Mount string // URL prefix, e.g. "/" or "/assets"
	Dir   string // local filesystem directory
}

type corsConfig struct {
	Origins     []string
	Methods     []string
	Headers     []string
	Credentials bool
	MaxAge      int
}

// rateLimitConfig is a per-IP token bucket. `Capacity` requests are
// allowed in any rolling `Window`, refilled linearly.
type rateLimitConfig struct {
	Capacity int
	Window   time.Duration
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func (rl *rateLimitConfig) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok {
		b = &tokenBucket{tokens: float64(rl.Capacity), last: now}
		rl.buckets[ip] = b
	}
	// Refill linearly based on elapsed time.
	elapsed := now.Sub(b.last).Seconds()
	refillRate := float64(rl.Capacity) / rl.Window.Seconds()
	b.tokens = math.Min(float64(rl.Capacity), b.tokens+elapsed*refillRate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

type Interpreter struct {
	globals     *Env
	routes      []registeredRoute
	middlewares map[string]*parser.MiddlewareDecl
	useGlobal   []string
	statics     []staticMount

	serverPort         int
	serverHost         string
	serverTLSCert      string
	serverTLSKey       string
	serverReadTimeout  time.Duration
	serverWriteTimeout time.Duration
	serverMaxBody      int64
	serverLog          bool
	serverCORS         *corsConfig
	serverRateLimit    *rateLimitConfig
	serverCompression  bool

	cliPort int // when > 0, overrides anything the program sets in its `server` block.
	file    string

	// callStack tracks active user-defined function calls so runtime
	// errors can include a traceback.
	callStack []StackFrame
}

// New constructs an interpreter pre-populated with all built-ins.
func New() *Interpreter {
	i := &Interpreter{
		globals:            NewEnv(nil),
		middlewares:        map[string]*parser.MiddlewareDecl{},
		serverPort:         8080,
		serverHost:         "0.0.0.0",
		serverReadTimeout:  10 * time.Second,
		serverWriteTimeout: 30 * time.Second,
		serverMaxBody:      10 * 1024 * 1024, // 10 MiB default
	}
	registerBuiltins(i)
	return i
}

// SetFile records the source file path for error messages.
func (i *Interpreter) SetFile(path string) { i.file = path }

// Globals returns the interpreter's top-level environment. It's exposed so
// embedders (notably the REPL) can evaluate statements that read or write
// the same scope across multiple calls.
func (i *Interpreter) Globals() *Env { return i.globals }

// Exec runs every statement in the program against the global scope and
// returns the value of the last expression statement (if any). Unlike Run,
// it does NOT start an HTTP server even if the program defined routes.
// This is intended for the REPL, where we want immediate feedback.
func (i *Interpreter) Exec(prog *parser.Program) (Value, error) {
	var last Value = NullValue()
	for _, s := range prog.Stmts {
		// Expression statements get evaluated directly so we can return
		// the result for the REPL to display. Other statements use
		// execStmt's normal path (which would discard the value).
		if es, ok := s.(*parser.ExprStmt); ok {
			v, err := i.evalExpr(es.Expr, i.globals)
			if err != nil {
				return Value{}, i.wrapErr(err)
			}
			last = v
			continue
		}
		if err := i.execStmt(s, i.globals); err != nil {
			if rs, ok := err.(*returnSignal); ok {
				last = rs.Value
				continue
			}
			return Value{}, i.wrapErr(err)
		}
	}
	return last, nil
}

// DisplayValue formats a value for human-readable output, used by the REPL.
func DisplayValue(v Value) string { return v.Display() }

// CallByName invokes a user-defined function in the global scope by name.
// Used by the test runner to call discovered `test_*` functions.
func (i *Interpreter) CallByName(name string, args []Value) (Value, error) {
	v, ok := i.globals.Get(name)
	if !ok {
		return Value{}, fmt.Errorf("undefined function %q", name)
	}
	if v.Kind != KindFunction {
		return Value{}, fmt.Errorf("%q is not a function", name)
	}
	return i.callFunction(nil, v.Function, args)
}

// SetPort marks the CLI-provided port. It overrides any port set by the
// program's `server { port: ... }` block so `mx run --port 3000` always wins.
func (i *Interpreter) SetPort(p int) {
	i.cliPort = p
	i.serverPort = p
}

// Load evaluates every top-level statement in the program against the
// global scope. It registers any `server { ... }` config, route, middleware
// and `static` mount, but does NOT start an HTTP server.
//
// Embedders that want to mount the resulting routes in their own server
// (for example, the Vercel adapter generated by `mx build --vercel`)
// should call Load followed by Handler.
func (i *Interpreter) Load(prog *parser.Program) error {
	for _, stmt := range prog.Stmts {
		if err := i.execStmt(stmt, i.globals); err != nil {
			if rs, ok := err.(*returnSignal); ok {
				_ = rs
				continue
			}
			return i.wrapErr(err)
		}
	}
	if i.cliPort > 0 {
		i.serverPort = i.cliPort
	}
	return nil
}

// HasRoutes reports whether the loaded program defined any HTTP routes
// or static mounts. Useful for embedders deciding whether to start a server.
func (i *Interpreter) HasRoutes() bool {
	return len(i.routes) > 0 || len(i.statics) > 0
}

// Run executes a parsed program. If the program declared any routes,
// it boots an HTTP server and blocks; otherwise it returns once the
// top-level statements have all been evaluated.
func (i *Interpreter) Run(prog *parser.Program) error {
	if err := i.Load(prog); err != nil {
		return err
	}
	if i.HasRoutes() {
		return i.startServer()
	}
	return nil
}

func (i *Interpreter) wrapErr(err error) error {
	var mx *MXError
	if errors.As(err, &mx) {
		mx.File = i.file
		if mx.Stack == nil && len(i.callStack) > 0 {
			mx.Stack = append([]StackFrame(nil), i.callStack...)
		}
		return mx
	}
	return err
}

// ===== Statement execution =====

func (i *Interpreter) execStmt(s parser.Stmt, env *Env) error {
	switch n := s.(type) {
	case *parser.LetStmt:
		v, err := i.evalExpr(n.Value, env)
		if err != nil {
			return err
		}
		if n.Pattern != nil {
			return i.bindDestructure(n, n.Pattern, v, env)
		}
		env.Set(n.Name, v)
	case *parser.AssignStmt:
		return i.execAssign(n, env)
	case *parser.FnDecl:
		fn := &Function{Name: n.Name, Params: n.Params, Body: n.Body, Closure: env}
		env.Set(n.Name, FunctionValue(fn))
	case *parser.ServerBlock:
		return i.execServer(n, env)
	case *parser.RouteDecl:
		i.registerRoute(n)
	case *parser.MiddlewareDecl:
		i.middlewares[n.Name] = n
	case *parser.UseStmt:
		i.useGlobal = append(i.useGlobal, n.Name)
	case *parser.IfStmt:
		return i.execIf(n, env)
	case *parser.LoopStmt:
		return i.execLoop(n, env)
	case *parser.WhileStmt:
		return i.execWhile(n, env)
	case *parser.BreakStmt:
		return &breakSignal{}
	case *parser.ContinueStmt:
		return &continueSignal{}
	case *parser.TryStmt:
		return i.execTry(n, env)
	case *parser.ReturnStmt:
		var v Value = NullValue()
		if n.Value != nil {
			rv, err := i.evalExpr(n.Value, env)
			if err != nil {
				return err
			}
			v = rv
		}
		return &returnSignal{Value: v}
	case *parser.ImportStmt:
		return i.execImport(n, env)
	case *parser.StaticStmt:
		i.statics = append(i.statics, staticMount{Mount: n.Mount, Dir: n.Dir})
	case *parser.SpawnStmt:
		// Run the body in a fresh goroutine. The body shares this
		// goroutine's env via closure — Env is RWMutex-protected, but
		// users should still prefer channels for coordination.
		body := n.Body
		go func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "[mx spawn panic] %v\n", r)
				}
			}()
			scope := NewEnv(env)
			for _, s := range body {
				if err := i.execStmt(s, scope); err != nil {
					if _, ok := err.(*returnSignal); ok {
						return
					}
					fmt.Fprintf(os.Stderr, "[mx spawn] %v\n", i.wrapErr(err))
					return
				}
			}
		}()
	case *parser.ExprStmt:
		_, err := i.evalExpr(n.Expr, env)
		return err
	default:
		return runtimeErrorf(s, "unsupported statement type %T", s)
	}
	return nil
}

// bindDestructure unpacks a value into multiple env bindings according
// to the pattern. Missing keys / out-of-range indexes produce null.
func (i *Interpreter) bindDestructure(n parser.Node, p *parser.DestructurePattern, v Value, env *Env) error {
	if p.IsArray {
		if v.Kind != KindArray {
			return runtimeErrorf(n, "cannot array-destructure %s", v.typeName())
		}
		for k, name := range p.Names {
			if k < len(v.Array) {
				env.Set(name, v.Array[k])
			} else {
				env.Set(name, NullValue())
			}
		}
		return nil
	}
	if v.Kind != KindObject {
		return runtimeErrorf(n, "cannot object-destructure %s", v.typeName())
	}
	for _, name := range p.Names {
		val, ok := v.Object.Get(name)
		if !ok {
			env.Set(name, NullValue())
			continue
		}
		env.Set(name, val)
	}
	return nil
}

func (i *Interpreter) execAssign(n *parser.AssignStmt, env *Env) error {
	val, err := i.evalExpr(n.Value, env)
	if err != nil {
		return err
	}
	switch t := n.Target.(type) {
	case *parser.Identifier:
		env.Assign(t.Name, val)
		return nil
	case *parser.MemberExpr:
		obj, err := i.evalExpr(t.Object, env)
		if err != nil {
			return err
		}
		if obj.Kind != KindObject {
			return runtimeErrorf(n, "cannot assign property on %s", obj.typeName())
		}
		obj.Object.Set(t.Property, val)
		return nil
	case *parser.IndexExpr:
		obj, err := i.evalExpr(t.Object, env)
		if err != nil {
			return err
		}
		idx, err := i.evalExpr(t.Index, env)
		if err != nil {
			return err
		}
		switch obj.Kind {
		case KindArray:
			if idx.Kind != KindNumber {
				return runtimeErrorf(n, "array index must be a number")
			}
			i2 := int(idx.Number)
			if i2 < 0 || i2 >= len(obj.Array) {
				return runtimeErrorf(n, "array index %d out of range", i2)
			}
			obj.Array[i2] = val
			return nil
		case KindObject:
			if idx.Kind != KindString {
				return runtimeErrorf(n, "object index must be a string")
			}
			obj.Object.Set(idx.String, val)
			return nil
		}
		return runtimeErrorf(n, "cannot index assign on %s", obj.typeName())
	}
	return runtimeErrorf(n, "invalid assignment target")
}

func (i *Interpreter) execServer(n *parser.ServerBlock, env *Env) error {
	for _, p := range n.Settings {
		v, err := i.evalExpr(p.Value, env)
		if err != nil {
			return err
		}
		switch p.Key {
		case "port":
			if v.Kind != KindNumber {
				return runtimeErrorf(n, "server.port must be a number")
			}
			i.serverPort = int(v.Number)
		case "host":
			if v.Kind != KindString {
				return runtimeErrorf(n, "server.host must be a string")
			}
			i.serverHost = v.String
		case "read_timeout":
			d, err := durationFromValue(v)
			if err != nil {
				return runtimeErrorf(n, "server.read_timeout: %v", err)
			}
			i.serverReadTimeout = d
		case "write_timeout":
			d, err := durationFromValue(v)
			if err != nil {
				return runtimeErrorf(n, "server.write_timeout: %v", err)
			}
			i.serverWriteTimeout = d
		case "max_body":
			n2, err := byteSizeFromValue(v)
			if err != nil {
				return runtimeErrorf(n, "server.max_body: %v", err)
			}
			i.serverMaxBody = n2
		case "tls":
			if v.Kind != KindObject {
				return runtimeErrorf(n, "server.tls must be an object with cert and key paths")
			}
			if cert, ok := v.Object.Get("cert"); ok && cert.Kind == KindString {
				i.serverTLSCert = cert.String
			}
			if key, ok := v.Object.Get("key"); ok && key.Kind == KindString {
				i.serverTLSKey = key.String
			}
		case "log":
			if v.Kind != KindBool {
				return runtimeErrorf(n, "server.log must be true or false")
			}
			i.serverLog = v.Bool
		case "compression":
			if v.Kind != KindBool {
				return runtimeErrorf(n, "server.compression must be true or false")
			}
			i.serverCompression = v.Bool
		case "rate_limit":
			if v.Kind != KindObject {
				return runtimeErrorf(n, "server.rate_limit must be an object")
			}
			cfg := &rateLimitConfig{Capacity: 60, Window: time.Minute, buckets: map[string]*tokenBucket{}}
			if v2, ok := v.Object.Get("requests"); ok && v2.Kind == KindNumber {
				cfg.Capacity = int(v2.Number)
			}
			if v2, ok := v.Object.Get("per"); ok {
				d, err := durationFromValue(v2)
				if err != nil {
					return runtimeErrorf(n, "rate_limit.per: %v", err)
				}
				cfg.Window = d
			}
			i.serverRateLimit = cfg
		case "cors":
			if v.Kind != KindObject {
				return runtimeErrorf(n, "server.cors must be an object")
			}
			cfg := &corsConfig{
				Origins: []string{"*"},
				Methods: []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
				MaxAge:  3600,
			}
			if v2, ok := v.Object.Get("origins"); ok && v2.Kind == KindArray {
				cfg.Origins = stringsFromArray(v2.Array)
			}
			if v2, ok := v.Object.Get("methods"); ok && v2.Kind == KindArray {
				cfg.Methods = stringsFromArray(v2.Array)
			}
			if v2, ok := v.Object.Get("headers"); ok && v2.Kind == KindArray {
				cfg.Headers = stringsFromArray(v2.Array)
			}
			if v2, ok := v.Object.Get("credentials"); ok && v2.Kind == KindBool {
				cfg.Credentials = v2.Bool
			}
			if v2, ok := v.Object.Get("max_age"); ok && v2.Kind == KindNumber {
				cfg.MaxAge = int(v2.Number)
			}
			i.serverCORS = cfg
		}
	}
	return nil
}

func stringsFromArray(arr []Value) []string {
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if v.Kind == KindString {
			out = append(out, v.String)
		}
	}
	return out
}

// durationFromValue accepts either a number of milliseconds or a string
// like "10s", "500ms", "2m" (passed straight to time.ParseDuration).
func durationFromValue(v Value) (time.Duration, error) {
	switch v.Kind {
	case KindNumber:
		return time.Duration(v.Number) * time.Millisecond, nil
	case KindString:
		return time.ParseDuration(v.String)
	}
	return 0, fmt.Errorf("expected number (ms) or string like \"5s\", got %s", v.typeName())
}

// byteSizeFromValue accepts a raw number (bytes) or a string like
// "10MB" / "512KB" / "1GB". Bytes only — no fractional sizes.
func byteSizeFromValue(v Value) (int64, error) {
	switch v.Kind {
	case KindNumber:
		return int64(v.Number), nil
	case KindString:
		s := strings.ToUpper(strings.TrimSpace(v.String))
		multiplier := int64(1)
		switch {
		case strings.HasSuffix(s, "GB"):
			multiplier = 1024 * 1024 * 1024
			s = strings.TrimSuffix(s, "GB")
		case strings.HasSuffix(s, "MB"):
			multiplier = 1024 * 1024
			s = strings.TrimSuffix(s, "MB")
		case strings.HasSuffix(s, "KB"):
			multiplier = 1024
			s = strings.TrimSuffix(s, "KB")
		case strings.HasSuffix(s, "B"):
			s = strings.TrimSuffix(s, "B")
		}
		s = strings.TrimSpace(s)
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid byte size %q", v.String)
		}
		return n * multiplier, nil
	}
	return 0, fmt.Errorf("expected number or size string, got %s", v.typeName())
}

func (i *Interpreter) registerRoute(n *parser.RouteDecl) {
	// Extract `use foo` middleware references from the route body.
	var mws []string
	var rest []parser.Stmt
	for _, s := range n.Body {
		if u, ok := s.(*parser.UseStmt); ok {
			mws = append(mws, u.Name)
			continue
		}
		rest = append(rest, s)
	}
	parts := splitPath(n.Path)
	i.routes = append(i.routes, registeredRoute{
		Method:      strings.ToUpper(n.Method),
		PathParts:   parts,
		Body:        rest,
		Middlewares: mws,
	})
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return []string{}
	}
	return strings.Split(p, "/")
}

func (i *Interpreter) execIf(n *parser.IfStmt, env *Env) error {
	c, err := i.evalExpr(n.Cond, env)
	if err != nil {
		return err
	}
	if c.IsTruthy() {
		return i.execBlock(n.Then, NewEnv(env))
	}
	if n.Else != nil {
		return i.execBlock(n.Else, NewEnv(env))
	}
	return nil
}

func (i *Interpreter) execBlock(stmts []parser.Stmt, env *Env) error {
	for _, s := range stmts {
		if err := i.execStmt(s, env); err != nil {
			return err
		}
	}
	return nil
}

func (i *Interpreter) execLoop(n *parser.LoopStmt, env *Env) error {
	iter, err := i.evalExpr(n.Iterable, env)
	if err != nil {
		return err
	}
	runIteration := func(idx int, item Value) (stop bool, err error) {
		scope := NewEnv(env)
		scope.Set(n.Var, item)
		if n.IndexVar != "" {
			scope.Set(n.IndexVar, NumberValue(float64(idx)))
		}
		if err := i.execBlock(n.Body, scope); err != nil {
			if _, ok := err.(*breakSignal); ok {
				return true, nil
			}
			if _, ok := err.(*continueSignal); ok {
				return false, nil
			}
			return false, err
		}
		return false, nil
	}
	switch iter.Kind {
	case KindArray:
		for k, item := range iter.Array {
			stop, err := runIteration(k, item)
			if err != nil {
				return err
			}
			if stop {
				return nil
			}
		}
	case KindNumber:
		count := int(iter.Number)
		for k := 0; k < count; k++ {
			stop, err := runIteration(k, NumberValue(float64(k)))
			if err != nil {
				return err
			}
			if stop {
				return nil
			}
		}
	case KindObject:
		for k, key := range iter.Object.Keys {
			stop, err := runIteration(k, StringValue(key))
			if err != nil {
				return err
			}
			if stop {
				return nil
			}
		}
	default:
		return runtimeErrorf(n, "cannot loop over %s", iter.typeName())
	}
	return nil
}

func (i *Interpreter) execWhile(n *parser.WhileStmt, env *Env) error {
	for {
		c, err := i.evalExpr(n.Cond, env)
		if err != nil {
			return err
		}
		if !c.IsTruthy() {
			return nil
		}
		scope := NewEnv(env)
		if err := i.execBlock(n.Body, scope); err != nil {
			if _, ok := err.(*breakSignal); ok {
				return nil
			}
			if _, ok := err.(*continueSignal); ok {
				continue
			}
			return err
		}
	}
}

func (i *Interpreter) execTry(n *parser.TryStmt, env *Env) error {
	tryEnv := NewEnv(env)
	err := i.execBlock(n.Try, tryEnv)
	if err == nil {
		return nil
	}
	if _, ok := err.(*returnSignal); ok {
		return err
	}
	catchEnv := NewEnv(env)
	if n.CatchVar != "" {
		errObj := NewOrderedMap()
		var msg string
		var mx *MXError
		if errors.As(err, &mx) {
			msg = mx.Message
		} else {
			msg = err.Error()
		}
		errObj.Set("message", StringValue(msg))
		catchEnv.Set(n.CatchVar, ObjectValue(errObj))
	}
	return i.execBlock(n.Catch, catchEnv)
}

func (i *Interpreter) execImport(n *parser.ImportStmt, env *Env) error {
	// Phase: minimal local-file import. Resolve relative to the running file.
	path := n.Path
	if i.file != "" {
		if !strings.HasPrefix(path, "/") {
			dir := i.file
			if idx := strings.LastIndex(dir, "/"); idx >= 0 {
				dir = dir[:idx]
				path = dir + "/" + path
			}
		}
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return runtimeErrorf(n, "cannot import %q: %v", n.Path, err)
	}
	prog, err := ParseSource(string(src))
	if err != nil {
		return runtimeErrorf(n, "import %q: %v", n.Path, err)
	}
	for _, s := range prog.Stmts {
		if err := i.execStmt(s, env); err != nil {
			return err
		}
	}
	return nil
}

// ===== Expression evaluation =====

func (i *Interpreter) evalExpr(e parser.Expr, env *Env) (Value, error) {
	switch n := e.(type) {
	case *parser.NumberLit:
		return NumberValue(n.Value), nil
	case *parser.StringLit:
		return StringValue(n.Value), nil
	case *parser.BoolLit:
		return BoolValue(n.Value), nil
	case *parser.NullLit:
		return NullValue(), nil
	case *parser.Identifier:
		v, ok := env.Get(n.Name)
		if !ok {
			suggestion := suggestIdentifier(env, n.Name)
			if suggestion != "" {
				return Value{}, runtimeErrorf(n, "undefined identifier %q (did you mean %q?)", n.Name, suggestion)
			}
			return Value{}, runtimeErrorf(n, "undefined identifier %q", n.Name)
		}
		return v, nil
	case *parser.ArrayLit:
		var arr []Value
		for _, el := range n.Elements {
			if sp, ok := el.(*parser.SpreadExpr); ok {
				inner, err := i.evalExpr(sp.Inner, env)
				if err != nil {
					return Value{}, err
				}
				if inner.Kind != KindArray {
					return Value{}, runtimeErrorf(sp, "cannot spread %s into an array", inner.typeName())
				}
				arr = append(arr, inner.Array...)
				continue
			}
			v, err := i.evalExpr(el, env)
			if err != nil {
				return Value{}, err
			}
			arr = append(arr, v)
		}
		return ArrayValue(arr), nil
	case *parser.ObjectLit:
		om := NewOrderedMap()
		for _, p := range n.Pairs {
			// Empty key marks a `...source` spread.
			if p.Key == "" {
				inner, err := i.evalExpr(p.Value, env)
				if err != nil {
					return Value{}, err
				}
				if inner.Kind != KindObject {
					return Value{}, runtimeErrorf(n, "cannot spread %s into an object", inner.typeName())
				}
				for _, k := range inner.Object.Keys {
					om.Set(k, inner.Object.Values[k])
				}
				continue
			}
			v, err := i.evalExpr(p.Value, env)
			if err != nil {
				return Value{}, err
			}
			om.Set(p.Key, v)
		}
		return ObjectValue(om), nil
	case *parser.UnaryExpr:
		v, err := i.evalExpr(n.Operand, env)
		if err != nil {
			return Value{}, err
		}
		switch n.Op {
		case "-":
			if v.Kind != KindNumber {
				return Value{}, runtimeErrorf(n, "unary - requires number, got %s", v.typeName())
			}
			return NumberValue(-v.Number), nil
		case "!":
			return BoolValue(!v.IsTruthy()), nil
		}
		return Value{}, runtimeErrorf(n, "unknown unary operator %q", n.Op)
	case *parser.BinaryExpr:
		return i.evalBinary(n, env)
	case *parser.CallExpr:
		return i.evalCall(n, env)
	case *parser.IndexExpr:
		obj, err := i.evalExpr(n.Object, env)
		if err != nil {
			return Value{}, err
		}
		idx, err := i.evalExpr(n.Index, env)
		if err != nil {
			return Value{}, err
		}
		return i.indexValue(n, obj, idx)
	case *parser.MemberExpr:
		obj, err := i.evalExpr(n.Object, env)
		if err != nil {
			return Value{}, err
		}
		if n.Optional && obj.Kind == KindNull {
			return NullValue(), nil
		}
		return i.memberAccess(n, obj, n.Property)
	case *parser.FnLit:
		return FunctionValue(&Function{Params: n.Params, Body: n.Body, Closure: env}), nil
	case *parser.MatchExpr:
		subj, err := i.evalExpr(n.Subject, env)
		if err != nil {
			return Value{}, err
		}
		for _, arm := range n.Arms {
			if arm.Pattern == nil {
				return i.evalExpr(arm.Body, env)
			}
			pat, err := i.evalExpr(arm.Pattern, env)
			if err != nil {
				return Value{}, err
			}
			if valuesEqual(subj, pat) {
				return i.evalExpr(arm.Body, env)
			}
		}
		return NullValue(), nil
	case *parser.TryExpr:
		tryEnv := NewEnv(env)
		v, err := i.execBlockAsValue(n.Try, tryEnv)
		if err == nil {
			return v, nil
		}
		if _, ok := err.(*returnSignal); ok {
			return Value{}, err
		}
		catchEnv := NewEnv(env)
		if n.CatchVar != "" {
			msg := err.Error()
			var mx *MXError
			if errors.As(err, &mx) {
				msg = mx.Message
			}
			errObj := NewOrderedMap()
			errObj.Set("message", StringValue(msg))
			catchEnv.Set(n.CatchVar, ObjectValue(errObj))
		}
		return i.execBlockAsValue(n.Catch, catchEnv)
	}
	return Value{}, fmt.Errorf("unknown expression node %T", e)
}

// execBlockAsValue runs a sequence of statements and returns the value of
// the last expression statement (or null if there isn't one). Used by
// `try` in expression position so the body can yield a value.
func (i *Interpreter) execBlockAsValue(stmts []parser.Stmt, env *Env) (Value, error) {
	var last Value = NullValue()
	for _, s := range stmts {
		if es, ok := s.(*parser.ExprStmt); ok {
			v, err := i.evalExpr(es.Expr, env)
			if err != nil {
				return Value{}, err
			}
			last = v
			continue
		}
		if err := i.execStmt(s, env); err != nil {
			if rs, ok := err.(*returnSignal); ok {
				return rs.Value, nil
			}
			return Value{}, err
		}
	}
	return last, nil
}

func (i *Interpreter) evalBinary(n *parser.BinaryExpr, env *Env) (Value, error) {
	if n.Op == "&&" {
		l, err := i.evalExpr(n.Left, env)
		if err != nil {
			return Value{}, err
		}
		if !l.IsTruthy() {
			return l, nil
		}
		return i.evalExpr(n.Right, env)
	}
	if n.Op == "||" {
		l, err := i.evalExpr(n.Left, env)
		if err != nil {
			return Value{}, err
		}
		if l.IsTruthy() {
			return l, nil
		}
		return i.evalExpr(n.Right, env)
	}
	if n.Op == "??" {
		l, err := i.evalExpr(n.Left, env)
		if err != nil {
			return Value{}, err
		}
		if l.Kind != KindNull {
			return l, nil
		}
		return i.evalExpr(n.Right, env)
	}

	l, err := i.evalExpr(n.Left, env)
	if err != nil {
		return Value{}, err
	}
	r, err := i.evalExpr(n.Right, env)
	if err != nil {
		return Value{}, err
	}

	switch n.Op {
	case "+":
		if l.Kind == KindString || r.Kind == KindString {
			return StringValue(l.Display() + r.Display()), nil
		}
		if l.Kind == KindNumber && r.Kind == KindNumber {
			return NumberValue(l.Number + r.Number), nil
		}
		if l.Kind == KindArray && r.Kind == KindArray {
			combined := append([]Value{}, l.Array...)
			combined = append(combined, r.Array...)
			return ArrayValue(combined), nil
		}
		return Value{}, runtimeErrorf(n, "cannot add %s and %s", l.typeName(), r.typeName())
	case "-", "*", "/", "%":
		if l.Kind != KindNumber || r.Kind != KindNumber {
			return Value{}, runtimeErrorf(n, "operator %s requires numbers", n.Op)
		}
		switch n.Op {
		case "-":
			return NumberValue(l.Number - r.Number), nil
		case "*":
			return NumberValue(l.Number * r.Number), nil
		case "/":
			if r.Number == 0 {
				return Value{}, runtimeErrorf(n, "division by zero")
			}
			return NumberValue(l.Number / r.Number), nil
		case "%":
			if r.Number == 0 {
				return Value{}, runtimeErrorf(n, "modulo by zero")
			}
			return NumberValue(math.Mod(l.Number, r.Number)), nil
		}
	case "==":
		return BoolValue(valuesEqual(l, r)), nil
	case "!=":
		return BoolValue(!valuesEqual(l, r)), nil
	case "<", ">", "<=", ">=":
		if l.Kind == KindNumber && r.Kind == KindNumber {
			switch n.Op {
			case "<":
				return BoolValue(l.Number < r.Number), nil
			case ">":
				return BoolValue(l.Number > r.Number), nil
			case "<=":
				return BoolValue(l.Number <= r.Number), nil
			case ">=":
				return BoolValue(l.Number >= r.Number), nil
			}
		}
		if l.Kind == KindString && r.Kind == KindString {
			switch n.Op {
			case "<":
				return BoolValue(l.String < r.String), nil
			case ">":
				return BoolValue(l.String > r.String), nil
			case "<=":
				return BoolValue(l.String <= r.String), nil
			case ">=":
				return BoolValue(l.String >= r.String), nil
			}
		}
		return Value{}, runtimeErrorf(n, "cannot compare %s and %s", l.typeName(), r.typeName())
	}
	return Value{}, runtimeErrorf(n, "unknown binary operator %q", n.Op)
}

func valuesEqual(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindNull:
		return true
	case KindBool:
		return a.Bool == b.Bool
	case KindNumber:
		return a.Number == b.Number
	case KindString:
		return a.String == b.String
	case KindArray:
		if len(a.Array) != len(b.Array) {
			return false
		}
		for i := range a.Array {
			if !valuesEqual(a.Array[i], b.Array[i]) {
				return false
			}
		}
		return true
	case KindObject:
		if len(a.Object.Keys) != len(b.Object.Keys) {
			return false
		}
		for _, k := range a.Object.Keys {
			bv, ok := b.Object.Values[k]
			if !ok {
				return false
			}
			if !valuesEqual(a.Object.Values[k], bv) {
				return false
			}
		}
		return true
	}
	return false
}

func (i *Interpreter) indexValue(n parser.Node, obj, idx Value) (Value, error) {
	switch obj.Kind {
	case KindArray:
		if idx.Kind != KindNumber {
			return Value{}, runtimeErrorf(n, "array index must be a number, got %s", idx.typeName())
		}
		k := int(idx.Number)
		if k < 0 {
			k += len(obj.Array)
		}
		if k < 0 || k >= len(obj.Array) {
			return NullValue(), nil
		}
		return obj.Array[k], nil
	case KindString:
		if idx.Kind != KindNumber {
			return Value{}, runtimeErrorf(n, "string index must be a number")
		}
		k := int(idx.Number)
		runes := []rune(obj.String)
		if k < 0 || k >= len(runes) {
			return NullValue(), nil
		}
		return StringValue(string(runes[k])), nil
	case KindObject:
		if idx.Kind != KindString {
			return Value{}, runtimeErrorf(n, "object index must be a string")
		}
		v, ok := obj.Object.Get(idx.String)
		if !ok {
			return NullValue(), nil
		}
		return v, nil
	}
	return Value{}, runtimeErrorf(n, "cannot index %s", obj.typeName())
}

func (i *Interpreter) memberAccess(n parser.Node, obj Value, prop string) (Value, error) {
	switch obj.Kind {
	case KindObject:
		v, ok := obj.Object.Get(prop)
		if !ok {
			return NullValue(), nil
		}
		return v, nil
	case KindArray:
		switch prop {
		case "length":
			return NumberValue(float64(len(obj.Array))), nil
		}
	case KindString:
		switch prop {
		case "length":
			return NumberValue(float64(len([]rune(obj.String)))), nil
		}
	}
	return Value{}, runtimeErrorf(n, "no property %q on %s", prop, obj.typeName())
}

func (i *Interpreter) evalCall(n *parser.CallExpr, env *Env) (Value, error) {
	callee, err := i.evalExpr(n.Callee, env)
	if err != nil {
		return Value{}, err
	}
	if callee.Kind != KindFunction {
		return Value{}, runtimeErrorf(n, "cannot call %s", callee.typeName())
	}
	var args []Value
	for _, a := range n.Args {
		if sp, ok := a.(*parser.SpreadExpr); ok {
			inner, err := i.evalExpr(sp.Inner, env)
			if err != nil {
				return Value{}, err
			}
			if inner.Kind != KindArray {
				return Value{}, runtimeErrorf(sp, "cannot spread %s as call arguments", inner.typeName())
			}
			args = append(args, inner.Array...)
			continue
		}
		v, err := i.evalExpr(a, env)
		if err != nil {
			return Value{}, err
		}
		args = append(args, v)
	}
	return i.callFunction(n, callee.Function, args)
}

func (i *Interpreter) callFunction(node parser.Node, fn *Function, args []Value) (Value, error) {
	if fn.Native != nil {
		return fn.Native(i, args)
	}
	// Record the call site so runtime errors carry a stack trace.
	frame := StackFrame{Name: fn.Name}
	if frame.Name == "" {
		frame.Name = "<anon>"
	}
	if node != nil {
		frame.Line, frame.Col = node.Pos()
	}
	i.callStack = append(i.callStack, frame)
	defer func() { i.callStack = i.callStack[:len(i.callStack)-1] }()

	scope := NewEnv(fn.Closure)
	for k, p := range fn.Params {
		if k < len(args) {
			scope.Set(p, args[k])
		} else {
			scope.Set(p, NullValue())
		}
	}
	for _, s := range fn.Body {
		err := i.execStmt(s, scope)
		if err != nil {
			if rs, ok := err.(*returnSignal); ok {
				return rs.Value, nil
			}
			// Capture the call stack into the error the first time it
			// passes through a function boundary. Subsequent re-raises
			// keep the original (deepest) snapshot.
			var mx *MXError
			if errors.As(err, &mx) && mx.Stack == nil {
				mx.Stack = append([]StackFrame(nil), i.callStack...)
			}
			return Value{}, err
		}
	}
	return NullValue(), nil
}

// ===== HTTP server =====

// Handler returns the fully-wrapped HTTP handler for the loaded program:
// the route mux composed with the configured CORS, logging, and request
// body size middleware.
//
// Call Load before Handler so the program's routes, middleware, and server
// config are registered. Handler is safe to call multiple times; each call
// returns a freshly-composed handler chain over the same underlying state.
//
// This is the entry point for embedders (e.g. the Vercel adapter) that want
// to mount an MX Script app inside their own HTTP server.
func (i *Interpreter) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", i.dispatch)

	// Compose the handler chain: cors -> logger -> max-body -> mux.
	handler := http.Handler(mux)
	if i.serverMaxBody > 0 {
		maxBody := i.serverMaxBody
		inner := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBody {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBody)
			}
			inner.ServeHTTP(w, r)
		})
	}
	if i.serverCompression {
		inner := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				inner.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")
			gz := gzip.NewWriter(w)
			defer gz.Close()
			inner.ServeHTTP(&gzipWriter{ResponseWriter: w, gz: gz}, r)
		})
	}
	if i.serverLog {
		inner := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, code: 200}
			inner.ServeHTTP(rec, r)
			fmt.Printf("\033[0;90m[%s]\033[0m %s %s \033[1;36m%d\033[0m \033[0;90m(%s)\033[0m\n",
				start.Format("15:04:05"), r.Method, r.URL.Path, rec.code, time.Since(start).Round(time.Microsecond))
		})
	}
	if i.serverRateLimit != nil {
		rl := i.serverRateLimit
		inner := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !rl.allow(ip) {
				w.Header().Set("Retry-After", strconv.Itoa(int(rl.Window.Seconds())))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			inner.ServeHTTP(w, r)
		})
	}
	if i.serverCORS != nil {
		cfg := i.serverCORS
		inner := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowOrigin := pickAllowedOrigin(cfg.Origins, origin)
			if allowOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
				if allowOrigin != "*" {
					w.Header().Set("Vary", "Origin")
				}
				if cfg.Credentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
			}
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.Methods, ", "))
				if len(cfg.Headers) > 0 {
					w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.Headers, ", "))
				} else if h := r.Header.Get("Access-Control-Request-Headers"); h != "" {
					w.Header().Set("Access-Control-Allow-Headers", h)
				}
				if cfg.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(cfg.MaxAge))
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			inner.ServeHTTP(w, r)
		})
	}
	return handler
}

func (i *Interpreter) startServer() error {
	addr := fmt.Sprintf("%s:%d", i.serverHost, i.serverPort)

	displayHost := i.serverHost
	if displayHost == "0.0.0.0" || displayHost == "" {
		displayHost = "localhost"
	}

	scheme := "http"
	if i.serverTLSCert != "" && i.serverTLSKey != "" {
		scheme = "https"
	}

	fmt.Printf("\n\033[1;32m🚀 MX Script\033[0m running at \033[1;36m%s://%s:%d\033[0m\n\n", scheme, displayHost, i.serverPort)
	if len(i.routes) > 0 {
		fmt.Println("\033[1;33mRoutes:\033[0m")
		for _, r := range i.routes {
			path := "/" + strings.Join(r.PathParts, "/")
			if path == "/" && len(r.PathParts) == 0 {
				path = "/"
			}
			fmt.Printf("  \033[1;35m%-6s\033[0m %s\n", r.Method, path)
		}
	}
	if len(i.statics) > 0 {
		fmt.Println("\033[1;33mStatic:\033[0m")
		for _, s := range i.statics {
			fmt.Printf("  \033[1;35m%-6s\033[0m %s -> %s\n", "FILES", s.Mount, s.Dir)
		}
	}
	fmt.Println()

	srv := &http.Server{
		Addr:              addr,
		Handler:           i.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       i.serverReadTimeout,
		WriteTimeout:      i.serverWriteTimeout,
	}

	// Graceful shutdown on SIGINT / SIGTERM. We give in-flight requests
	// up to 10 seconds to finish before forcefully closing.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		var err error
		if scheme == "https" {
			err = srv.ListenAndServeTLS(i.serverTLSCert, i.serverTLSKey)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case sig := <-stop:
		fmt.Printf("\n\033[1;33m[mx]\033[0m %v received — shutting down gracefully...\n", sig)
		ctx, cancel := contextWithTimeout(10 * time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
			return err
		}
		fmt.Printf("\033[1;32m[mx]\033[0m bye\n")
		return nil
	case err := <-errCh:
		return err
	}
}

func (i *Interpreter) dispatch(w http.ResponseWriter, r *http.Request) {
	for _, route := range i.routes {
		// SSE / WS routes accept GET requests but are tagged in their
		// own pseudo-method internally.
		if route.Method == "SSE" || route.Method == "WS" {
			if r.Method != http.MethodGet {
				continue
			}
		} else if route.Method != r.Method {
			continue
		}
		params, ok := matchPath(route.PathParts, r.URL.Path)
		if !ok {
			continue
		}
		switch route.Method {
		case "SSE":
			i.runSSE(w, r, route, params)
		case "WS":
			i.runWS(w, r, route, params)
		default:
			i.runRoute(w, r, route, params)
		}
		return
	}

	// Fall through to static mounts. Longest mount prefix wins so e.g.
	// `static "./api-docs" at "/docs"` is checked before `static "./public"`.
	if i.serveStatic(w, r) {
		return
	}

	http.NotFound(w, r)
}

// runWS upgrades the HTTP connection to WebSocket and runs the route
// body once with `recv`, `send`, and `close` injected. The route body
// is responsible for the read loop — typically:
//
//	ws /chat {
//	  while (true) {
//	    let msg = recv()
//	    if (msg == null) { break }   // peer closed
//	    send("echo: " + msg)
//	  }
//	}
func (i *Interpreter) runWS(w http.ResponseWriter, r *http.Request, route registeredRoute, params map[string]string) {
	ws, err := upgradeWebSocket(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer ws.WriteClose(1000, "")

	scope := NewEnv(i.globals)
	scope.Set("request", buildRequestObject(r, params))

	scope.Set("recv", FunctionValue(&Function{Name: "recv", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		msg, _, err := ws.ReadMessage()
		if err != nil {
			return NullValue(), nil // peer closed → null sentinel for the route loop
		}
		return StringValue(msg), nil
	}}))
	scope.Set("send", FunctionValue(&Function{Name: "send", Native: func(_ *Interpreter, args []Value) (Value, error) {
		var s string
		if len(args) > 0 {
			if args[0].Kind == KindString {
				s = args[0].String
			} else {
				b, err := jsonEncode(args[0])
				if err != nil {
					return Value{}, err
				}
				s = string(b)
			}
		}
		return NullValue(), ws.WriteText(s)
	}}))
	scope.Set("close", FunctionValue(&Function{Name: "close", Native: func(_ *Interpreter, args []Value) (Value, error) {
		code := 1000
		reason := ""
		if len(args) > 0 && args[0].Kind == KindNumber {
			code = int(args[0].Number)
		}
		if len(args) > 1 && args[1].Kind == KindString {
			reason = args[1].String
		}
		return NullValue(), ws.WriteClose(code, reason)
	}}))

	for _, s := range route.Body {
		if err := i.execStmt(s, scope); err != nil {
			if _, ok := err.(*returnSignal); ok {
				return
			}
			fmt.Fprintf(os.Stderr, "[mx ws] %v\n", i.wrapErr(err))
			return
		}
	}
}

// runSSE handles a server-sent-events route. The route body is executed
// once per connection with a `send(value)` function injected. send writes
// one SSE frame per call and flushes; the connection stays open until the
// route body finishes or the client disconnects.
func (i *Interpreter) runSSE(w http.ResponseWriter, r *http.Request, route registeredRoute, params map[string]string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	scope := NewEnv(i.globals)
	scope.Set("request", buildRequestObject(r, params))
	ctx := r.Context()

	send := &Function{Name: "send", Native: func(_ *Interpreter, args []Value) (Value, error) {
		if ctx.Err() != nil {
			return Value{}, ctx.Err()
		}
		var v Value = NullValue()
		if len(args) > 0 {
			v = args[0]
		}
		body, err := jsonEncode(v)
		if err != nil {
			return Value{}, err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", string(body)); err != nil {
			return Value{}, err
		}
		flusher.Flush()
		return NullValue(), nil
	}}
	scope.Set("send", FunctionValue(send))

	for _, s := range route.Body {
		if err := i.execStmt(s, scope); err != nil {
			if _, ok := err.(*returnSignal); ok {
				return
			}
			fmt.Fprintf(os.Stderr, "[mx] sse error: %v\n", i.wrapErr(err))
			return
		}
	}
}

func (i *Interpreter) serveStatic(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	type candidate struct {
		mount string
		dir   string
	}
	var best candidate
	bestLen := -1
	for _, s := range i.statics {
		mount := s.Mount
		if mount == "" {
			mount = "/"
		}
		if !strings.HasPrefix(r.URL.Path, mount) {
			continue
		}
		// Match longest mount prefix.
		if len(mount) > bestLen {
			best = candidate{mount: mount, dir: s.Dir}
			bestLen = len(mount)
		}
	}
	if bestLen < 0 {
		return false
	}
	rel := strings.TrimPrefix(r.URL.Path, best.mount)
	rel = strings.TrimPrefix(rel, "/")
	// Path-traversal guard.
	if strings.Contains(rel, "..") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return true
	}
	full := filepath.Join(best.dir, rel)
	info, err := os.Stat(full)
	if err != nil {
		return false
	}
	if info.IsDir() {
		// Try index.html in the directory.
		idx := filepath.Join(full, "index.html")
		if _, err := os.Stat(idx); err == nil {
			http.ServeFile(w, r, idx)
			return true
		}
		return false
	}
	http.ServeFile(w, r, full)
	return true
}

func matchPath(parts []string, urlPath string) (map[string]string, bool) {
	segs := splitPath(urlPath)
	if len(segs) != len(parts) {
		return nil, false
	}
	params := map[string]string{}
	for k, p := range parts {
		if strings.HasPrefix(p, ":") {
			params[p[1:]] = segs[k]
			continue
		}
		if p != segs[k] {
			return nil, false
		}
	}
	return params, true
}

func (i *Interpreter) runRoute(w http.ResponseWriter, r *http.Request, route registeredRoute, params map[string]string) {
	scope := NewEnv(i.globals)
	scope.Set("request", buildRequestObject(r, params))

	// Run global middlewares, then route-level middlewares.
	for _, mw := range append(i.useGlobal, route.Middlewares...) {
		decl, ok := i.middlewares[mw]
		if !ok {
			i.writeError(w, fmt.Errorf("unknown middleware %q", mw))
			return
		}
		mwScope := NewEnv(scope)
		for _, s := range decl.Body {
			if err := i.execStmt(s, mwScope); err != nil {
				if rs, ok := err.(*returnSignal); ok {
					if rs.Value.Kind != KindNull {
						writeResponse(w, rs.Value)
						return
					}
					break
				}
				i.writeError(w, err)
				return
			}
		}
	}

	for _, s := range route.Body {
		if err := i.execStmt(s, scope); err != nil {
			if rs, ok := err.(*returnSignal); ok {
				writeResponse(w, rs.Value)
				return
			}
			i.writeError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (i *Interpreter) writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	body, _ := json.Marshal(map[string]string{"error": err.Error()})
	_, _ = w.Write(body)
	fmt.Fprintf(os.Stderr, "[mx] runtime error: %s\n", i.wrapErr(err).Error())
}

func writeResponse(w http.ResponseWriter, v Value) {
	if v.Kind == KindResponse {
		resp := v.Response
		ct := resp.ContentType
		if ct == "" {
			ct = "application/json"
		}
		w.Header().Set("Content-Type", ct)
		for k, vv := range resp.Headers {
			w.Header().Set(k, vv)
		}
		for _, c := range resp.Cookies {
			http.SetCookie(w, c)
		}
		status := resp.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		body, err := jsonEncode(resp.Body)
		if err == nil {
			if resp.Body.Kind == KindString && ct != "application/json" {
				_, _ = w.Write([]byte(resp.Body.String))
				return
			}
			_, _ = w.Write(body)
		}
		return
	}
	if v.Kind == KindString {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(v.String))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	body, err := jsonEncode(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(body)
}

func buildRequestObject(r *http.Request, params map[string]string) Value {
	req := NewOrderedMap()
	req.Set("method", StringValue(r.Method))
	req.Set("path", StringValue(r.URL.Path))

	headers := NewOrderedMap()
	keys := make([]string, 0, len(r.Header))
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		headers.Set(strings.ToLower(k), StringValue(r.Header.Get(k)))
	}
	req.Set("headers", ObjectValue(headers))

	q := NewOrderedMap()
	queryKeys := make([]string, 0, len(r.URL.Query()))
	for k := range r.URL.Query() {
		queryKeys = append(queryKeys, k)
	}
	sort.Strings(queryKeys)
	for _, k := range queryKeys {
		q.Set(k, StringValue(r.URL.Query().Get(k)))
	}
	req.Set("query", ObjectValue(q))

	pm := NewOrderedMap()
	pmKeys := make([]string, 0, len(params))
	for k := range params {
		pmKeys = append(pmKeys, k)
	}
	sort.Strings(pmKeys)
	for _, k := range pmKeys {
		pm.Set(k, StringValue(params[k]))
	}
	req.Set("params", ObjectValue(pm))

	cookies := NewOrderedMap()
	for _, c := range r.Cookies() {
		cookies.Set(c.Name, StringValue(c.Value))
	}
	req.Set("cookies", ObjectValue(cookies))

	// Convenience helpers most apps end up writing themselves.
	authHeader := r.Header.Get("Authorization")
	bearer := ""
	if strings.HasPrefix(authHeader, "Bearer ") {
		bearer = strings.TrimPrefix(authHeader, "Bearer ")
	}
	req.Set("bearer_token", StringValue(bearer))
	req.Set("is_json", BoolValue(strings.Contains(r.Header.Get("Content-Type"), "application/json")))
	req.Set("ip", StringValue(clientIP(r)))

	bodyVal := NullValue()
	filesVal := NullValue()
	if r.Body != nil {
		ct := r.Header.Get("Content-Type")
		// Multipart needs the original reader; everything else can be slurped.
		if strings.HasPrefix(ct, "multipart/form-data") {
			fields, files, err := parseMultipart(r)
			if err == nil {
				bodyVal = ObjectValue(fields)
				filesVal = ObjectValue(files)
			}
		} else {
			raw, err := io.ReadAll(r.Body)
			if err == nil && len(raw) > 0 {
				switch {
				case strings.Contains(ct, "application/json"):
					if v, err := jsonDecode(raw); err == nil {
						bodyVal = v
					} else {
						bodyVal = StringValue(string(raw))
					}
				case strings.Contains(ct, "application/x-www-form-urlencoded"):
					if vals, err := url.ParseQuery(string(raw)); err == nil {
						form := NewOrderedMap()
						fk := make([]string, 0, len(vals))
						for k := range vals {
							fk = append(fk, k)
						}
						sort.Strings(fk)
						for _, k := range fk {
							form.Set(k, StringValue(vals.Get(k)))
						}
						bodyVal = ObjectValue(form)
					} else {
						bodyVal = StringValue(string(raw))
					}
				default:
					bodyVal = StringValue(string(raw))
				}
			}
		}
	}
	req.Set("body", bodyVal)
	req.Set("files", filesVal)

	return ObjectValue(req)
}

// ===== JSON encoding / decoding =====

func jsonEncode(v Value) ([]byte, error) {
	g, err := valueToGo(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(g); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out, nil
}

func valueToGo(v Value) (any, error) {
	switch v.Kind {
	case KindNull:
		return nil, nil
	case KindBool:
		return v.Bool, nil
	case KindNumber:
		if v.Number == math.Trunc(v.Number) && !math.IsInf(v.Number, 0) && math.Abs(v.Number) < 1e15 {
			return int64(v.Number), nil
		}
		return v.Number, nil
	case KindString:
		return v.String, nil
	case KindArray:
		out := make([]any, 0, len(v.Array))
		for _, el := range v.Array {
			g, err := valueToGo(el)
			if err != nil {
				return nil, err
			}
			out = append(out, g)
		}
		return out, nil
	case KindObject:
		// Use json.RawMessage trick to preserve insertion order.
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range v.Object.Keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			buf.Write(kb)
			buf.WriteByte(':')
			child, err := jsonEncode(v.Object.Values[k])
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		buf.WriteByte('}')
		return json.RawMessage(buf.Bytes()), nil
	case KindResponse:
		return valueToGo(v.Response.Body)
	}
	return nil, fmt.Errorf("cannot encode %s as JSON", v.typeName())
}

func jsonDecode(raw []byte) (Value, error) {
	var any interface{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&any); err != nil {
		return Value{}, err
	}
	return goToValue(any)
}

func goToValue(g interface{}) (Value, error) {
	switch x := g.(type) {
	case nil:
		return NullValue(), nil
	case bool:
		return BoolValue(x), nil
	case string:
		return StringValue(x), nil
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return Value{}, err
		}
		return NumberValue(f), nil
	case float64:
		return NumberValue(x), nil
	case []interface{}:
		var out []Value
		for _, e := range x {
			v, err := goToValue(e)
			if err != nil {
				return Value{}, err
			}
			out = append(out, v)
		}
		return ArrayValue(out), nil
	case map[string]interface{}:
		om := NewOrderedMap()
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v, err := goToValue(x[k])
			if err != nil {
				return Value{}, err
			}
			om.Set(k, v)
		}
		return ObjectValue(om), nil
	}
	return Value{}, fmt.Errorf("cannot convert %T to mx value", g)
}

// ===== Random init =====

func init() {
	rand.Seed(time.Now().UnixNano())
}

// ===== ParseSource convenience used by import =====

// ParseSource is implemented in parse_helper.go to avoid an import cycle warning.
