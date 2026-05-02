// interpreter_test.go covers end-to-end evaluation of MX Script programs.
// Each test parses a source string, runs it, and checks observable behavior
// (return values, side effects, error messages).
package interpreter

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func runWithGlobals(t *testing.T, src string) *Interpreter {
	t.Helper()
	prog, err := ParseSource(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	interp := New()
	for _, s := range prog.Stmts {
		if err := interp.execStmt(s, interp.globals); err != nil {
			if _, ok := err.(*returnSignal); ok {
				continue
			}
			t.Fatalf("exec: %v", err)
		}
	}
	return interp
}

func evalExpr(t *testing.T, src string) Value {
	t.Helper()
	interp := runWithGlobals(t, "let __result = "+src)
	v, ok := interp.globals.Get("__result")
	if !ok {
		t.Fatal("no __result in globals")
	}
	return v
}

func TestArithmetic(t *testing.T) {
	cases := map[string]float64{
		"1 + 2":       3,
		"10 - 3":      7,
		"4 * 5":       20,
		"20 / 4":      5,
		"10 % 3":      1,
		"2 + 3 * 4":   14,
		"(2 + 3) * 4": 20,
		"-5 + 10":     5,
	}
	for src, want := range cases {
		v := evalExpr(t, src)
		if v.Kind != KindNumber || v.Number != want {
			t.Errorf("%s: got %v, want %v", src, v, want)
		}
	}
}

func TestStringConcat(t *testing.T) {
	v := evalExpr(t, `"hello, " + "world"`)
	if v.Kind != KindString || v.String != "hello, world" {
		t.Errorf("got %v, want hello, world", v)
	}
	v = evalExpr(t, `"answer: " + 42`)
	if v.Kind != KindString || v.String != "answer: 42" {
		t.Errorf("got %v", v)
	}
}

func TestBooleanLogic(t *testing.T) {
	cases := map[string]bool{
		"true && false": false,
		"true || false": true,
		"!true":         false,
		"1 == 1":        true,
		"1 != 2":        true,
		"3 < 5":         true,
		"5 >= 5":        true,
	}
	for src, want := range cases {
		v := evalExpr(t, src)
		if v.Kind != KindBool || v.Bool != want {
			t.Errorf("%s: got %v, want %v", src, v, want)
		}
	}
}

func TestFunctionsAndClosures(t *testing.T) {
	interp := runWithGlobals(t, `
fn make_counter() {
  let count = 0
  return fn() {
    count = count + 1
    return count
  }
}
let c = make_counter()
let a = c()
let b = c()
let cc = c()
`)
	for name, want := range map[string]float64{"a": 1, "b": 2, "cc": 3} {
		v, _ := interp.globals.Get(name)
		if v.Kind != KindNumber || v.Number != want {
			t.Errorf("%s: got %v, want %v", name, v, want)
		}
	}
}

func TestIfElse(t *testing.T) {
	interp := runWithGlobals(t, `
let x = 10
let label = ""
if (x > 5) {
  label = "big"
} else {
  label = "small"
}
`)
	v, _ := interp.globals.Get("label")
	if v.Kind != KindString || v.String != "big" {
		t.Errorf("got %v, want big", v)
	}
}

func TestLoopOverArray(t *testing.T) {
	interp := runWithGlobals(t, `
let total = 0
loop [1, 2, 3, 4] as n {
  total = total + n
}
`)
	v, _ := interp.globals.Get("total")
	if v.Kind != KindNumber || v.Number != 10 {
		t.Errorf("got %v, want 10", v)
	}
}

func TestArrayBuiltins(t *testing.T) {
	v := evalExpr(t, `len([1, 2, 3])`)
	if v.Number != 3 {
		t.Errorf("len: got %v, want 3", v)
	}
	v = evalExpr(t, `map([1, 2, 3], fn(n) { return n * 2 })`)
	if v.Kind != KindArray || len(v.Array) != 3 || v.Array[1].Number != 4 {
		t.Errorf("map: got %v", v)
	}
	v = evalExpr(t, `filter([1, 2, 3, 4], fn(n) { return n > 2 })`)
	if v.Kind != KindArray || len(v.Array) != 2 {
		t.Errorf("filter: got %v", v)
	}
	v = evalExpr(t, `find([1, 2, 3], fn(n) { return n == 2 })`)
	if v.Number != 2 {
		t.Errorf("find: got %v", v)
	}
}

func TestStringBuiltins(t *testing.T) {
	if v := evalExpr(t, `upper("hello")`); v.String != "HELLO" {
		t.Errorf("upper: got %v", v)
	}
	if v := evalExpr(t, `trim("  hi  ")`); v.String != "hi" {
		t.Errorf("trim: got %v", v)
	}
	if v := evalExpr(t, `contains("hello world", "world")`); !v.Bool {
		t.Errorf("contains: got %v", v)
	}
	if v := evalExpr(t, `len(split("a,b,c", ","))`); v.Number != 3 {
		t.Errorf("split: got %v", v)
	}
}

func TestObjectAccess(t *testing.T) {
	v := evalExpr(t, `({ id: 1, name: "Jassim" }).name`)
	if v.Kind != KindString || v.String != "Jassim" {
		t.Errorf("got %v", v)
	}
	v = evalExpr(t, `({ id: 1 })["id"]`)
	if v.Number != 1 {
		t.Errorf("got %v", v)
	}
}

func TestTryCatch(t *testing.T) {
	interp := runWithGlobals(t, `
let msg = ""
try {
  let n = num("not a number")
} catch (e) {
  msg = e.message
}
`)
	v, _ := interp.globals.Get("msg")
	if v.Kind != KindString || v.String == "" {
		t.Errorf("msg should be non-empty error message, got %v", v)
	}
}

func TestRouteDispatch(t *testing.T) {
	interp := runWithGlobals(t, `
route GET /hello/:name {
  return json({ greeting: "Hi " + request.params.name })
}
`)
	if len(interp.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(interp.routes))
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/hello/jassim", nil)
	interp.dispatch(rec, req)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "Hi jassim") {
		t.Errorf("body: got %q", body)
	}
}

func TestMiddlewareShortCircuit(t *testing.T) {
	interp := runWithGlobals(t, `
middleware auth {
  if (request.headers["x-key"] != "secret") {
    return status(401, { error: "denied" })
  }
}
route GET /private {
  use auth
  return json({ ok: true })
}
`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/private", nil)
	interp.dispatch(rec, req)
	if rec.Code != 401 {
		t.Errorf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/private", nil)
	req.Header.Set("X-Key", "secret")
	interp.dispatch(rec, req)
	if rec.Code != 200 {
		t.Errorf("expected 200 with auth, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestNotFound(t *testing.T) {
	interp := runWithGlobals(t, `route GET / { return json({ ok: true }) }`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/missing", nil)
	interp.dispatch(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestJSONOrderPreserved(t *testing.T) {
	v := evalExpr(t, `{ z: 1, a: 2, m: 3 }`)
	out, err := jsonEncode(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := string(out)
	want := `{"z":1,"a":2,"m":3}`
	if got != want {
		t.Errorf("ordered JSON: got %s, want %s", got, want)
	}
}

// TestEmbedderAPI verifies the Load + Handler + HasRoutes surface used by the
// Vercel adapter (and any other host that wants to mount an MX app inside its
// own HTTP server). The semantics must stay stable across versions.
func TestEmbedderAPI(t *testing.T) {
	src := `
route GET /ping {
  return json({ pong: true })
}
`
	prog, err := ParseSource(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	interp := New()
	if err := interp.Load(prog); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !interp.HasRoutes() {
		t.Fatal("HasRoutes should be true after loading a program with routes")
	}

	handler := interp.Handler()
	if handler == nil {
		t.Fatal("Handler returned nil")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ping", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"pong":true`) {
		t.Errorf("body: got %q, want pong:true", rec.Body.String())
	}
}

// TestHandlerWithoutRoutes confirms that an MX program with no routes still
// produces a usable handler (it just 404s on every path), so embedders don't
// have to special-case empty programs.
func TestHandlerWithoutRoutes(t *testing.T) {
	prog, err := ParseSource(`let x = 42`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	interp := New()
	if err := interp.Load(prog); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if interp.HasRoutes() {
		t.Error("HasRoutes should be false for a routeless program")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	interp.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 from empty handler, got %d", rec.Code)
	}
}

// evalCase runs `eval(<src>)` from MX-land and returns the structured result
// object so tests can assert against its fields.
func evalCase(t *testing.T, src string) *OrderedMap {
	t.Helper()
	prog, err := ParseSource(`let __r = eval(__src)`)
	if err != nil {
		t.Fatalf("harness parse: %v", err)
	}
	interp := New()
	interp.globals.Set("__src", StringValue(src))
	if _, err := interp.Exec(prog); err != nil {
		t.Fatalf("harness exec: %v", err)
	}
	v, _ := interp.globals.Get("__r")
	if v.Kind != KindObject {
		t.Fatalf("eval did not return an object, got %s", v.typeName())
	}
	return v.Object
}

func TestEvalCapturesOutput(t *testing.T) {
	r := evalCase(t, `print("hello")`+"\n"+`print(1 + 2)`)
	if ok, _ := r.Get("ok"); !ok.Bool {
		errV, _ := r.Get("error")
		t.Fatalf("expected ok=true, got error=%v", errV)
	}
	out, _ := r.Get("output")
	want := "hello\n3\n"
	if out.String != want {
		t.Errorf("output mismatch:\ngot  %q\nwant %q", out.String, want)
	}
}

func TestEvalReturnsParseError(t *testing.T) {
	r := evalCase(t, `let x = @@@`)
	if ok, _ := r.Get("ok"); ok.Bool {
		t.Fatal("expected ok=false on syntax garbage")
	}
	errV, _ := r.Get("error")
	if errV.Kind != KindString || errV.String == "" {
		t.Errorf("expected non-empty error string, got %#v", errV)
	}
}

func TestEvalReturnsRuntimeError(t *testing.T) {
	r := evalCase(t, `error("boom")`)
	if ok, _ := r.Get("ok"); ok.Bool {
		t.Fatal("expected ok=false when program raises an error")
	}
	errV, _ := r.Get("error")
	if !strings.Contains(errV.String, "boom") {
		t.Errorf("expected error to mention 'boom', got %q", errV.String)
	}
}

func TestEvalDoesNotStartServer(t *testing.T) {
	// `server { port: ... }` and route declarations must NOT actually bind a
	// listener — Exec only registers state. This is what makes eval() safe to
	// call from inside a request handler (e.g. the playground's POST /run).
	r := evalCase(t, `
server { port: 9999 }
get / { return json({ ok: true }) }
print("registered")
`)
	if ok, _ := r.Get("ok"); !ok.Bool {
		errV, _ := r.Get("error")
		t.Fatalf("ok=false unexpectedly: %v", errV)
	}
	out, _ := r.Get("output")
	if !strings.Contains(out.String, "registered") {
		t.Errorf("expected 'registered' in output, got %q", out.String)
	}
}

func TestEvalReportsDuration(t *testing.T) {
	r := evalCase(t, `let x = 1 + 1`)
	d, _ := r.Get("duration_ms")
	if d.Kind != KindNumber || d.Number < 0 {
		t.Errorf("expected non-negative duration_ms, got %#v", d)
	}
}

// TestOpenAICompatProvidersTable sanity-checks the dispatch table:
// every entry has a non-empty name, URL, env var (unless NoAuth), and
// default model. Catches typos that would otherwise only show up the
// first time a user actually calls the provider.
func TestOpenAICompatProvidersTable(t *testing.T) {
	if len(openAICompatProviders) == 0 {
		t.Fatal("expected at least one OpenAI-compat provider")
	}
	for key, p := range openAICompatProviders {
		if p.Name == "" {
			t.Errorf("provider %q: empty Name", key)
		}
		if p.BaseURL == "" {
			t.Errorf("provider %q: empty BaseURL", key)
		}
		if p.DefaultModel == "" {
			t.Errorf("provider %q: empty DefaultModel", key)
		}
		if !p.NoAuth && p.EnvKey == "" {
			t.Errorf("provider %q: missing EnvKey (and not NoAuth)", key)
		}
	}
	// Verify the seven providers we ship are actually wired up.
	expected := []string{"grok", "mistral", "deepseek", "groq", "openrouter", "together", "ollama"}
	for _, name := range expected {
		if _, ok := openAICompatProviders[name]; !ok {
			t.Errorf("provider %q missing from dispatch table", name)
		}
	}
}

// TestOpenAICompatRequiresKey confirms each non-NoAuth provider returns
// a clear error when its env var is missing — users hit this first.
func TestOpenAICompatRequiresKey(t *testing.T) {
	for key, p := range openAICompatProviders {
		if p.NoAuth {
			continue
		}
		// Stash and clear the env var so the call definitely fails.
		prev := os.Getenv(p.EnvKey)
		os.Unsetenv(p.EnvKey)
		_, err := aiCompleteOpenAICompat(p, "hi", "", 16)
		if prev != "" {
			os.Setenv(p.EnvKey, prev)
		}
		if err == nil {
			t.Errorf("provider %q: expected missing-key error, got nil", key)
			continue
		}
		if !strings.Contains(err.Error(), p.EnvKey) {
			t.Errorf("provider %q: error %q should mention %s", key, err.Error(), p.EnvKey)
		}
	}
}

func TestTemplateInterpolationAndEscape(t *testing.T) {
	vars := NewOrderedMap()
	vars.Set("title", StringValue("<script>alert(1)</script>"))
	vars.Set("desc", StringValue("hello"))
	out, err := renderTemplate("<h1>{{ title }}</h1><p>{{{ desc }}}</p>", vars, nil, true)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "<h1>&lt;script&gt;alert(1)&lt;/script&gt;</h1><p>hello</p>"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestTemplateIfElse(t *testing.T) {
	vars := NewOrderedMap()
	vars.Set("logged_in", BoolValue(true))
	tmpl := `{{#if logged_in}}hi{{else}}sign in{{/if}}`
	out, _ := renderTemplate(tmpl, vars, nil, true)
	if out != "hi" {
		t.Errorf("if=true: got %q", out)
	}
	vars.Set("logged_in", BoolValue(false))
	out, _ = renderTemplate(tmpl, vars, nil, true)
	if out != "sign in" {
		t.Errorf("if=false: got %q", out)
	}
}

func TestTemplateEachArrayOfObjects(t *testing.T) {
	post1 := NewOrderedMap()
	post1.Set("title", StringValue("First"))
	post1.Set("slug", StringValue("first"))
	post2 := NewOrderedMap()
	post2.Set("title", StringValue("Second"))
	post2.Set("slug", StringValue("second"))
	vars := NewOrderedMap()
	vars.Set("posts", ArrayValue([]Value{ObjectValue(post1), ObjectValue(post2)}))

	tmpl := `<ul>{{#each posts}}<li><a href="/{{slug}}">{{title}}</a></li>{{/each}}</ul>`
	out, err := renderTemplate(tmpl, vars, nil, true)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := `<ul><li><a href="/first">First</a></li><li><a href="/second">Second</a></li></ul>`
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestTemplateEachExposesIndexAndThis(t *testing.T) {
	vars := NewOrderedMap()
	vars.Set("nums", ArrayValue([]Value{NumberValue(10), NumberValue(20), NumberValue(30)}))
	tmpl := `{{#each nums}}{{@index}}={{this}};{{/each}}`
	out, _ := renderTemplate(tmpl, vars, nil, true)
	if out != "0=10;1=20;2=30;" {
		t.Errorf("got %q", out)
	}
}

func TestTemplateEachEmptyArrayRendersNothing(t *testing.T) {
	vars := NewOrderedMap()
	vars.Set("items", ArrayValue(nil))
	out, _ := renderTemplate(`pre{{#each items}}x{{/each}}post`, vars, nil, true)
	if out != "prepost" {
		t.Errorf("got %q, want \"prepost\"", out)
	}
}

func TestTemplatePartials(t *testing.T) {
	vars := NewOrderedMap()
	vars.Set("user", StringValue("Jassim"))
	tmpl := `<header>{{> nav}}</header><main>hi {{ user }}</main>`
	partials := map[string]string{
		"nav": `<a href="/">home</a>`,
	}
	out, err := renderTemplate(tmpl, vars, partials, true)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := `<header><a href="/">home</a></header><main>hi Jassim</main>`
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestTemplateMissingPartialErrors(t *testing.T) {
	_, err := renderTemplate(`{{> missing}}`, nil, map[string]string{}, true)
	if err == nil {
		t.Error("expected error for missing partial")
	}
}

func TestTemplateUnterminatedBlockErrors(t *testing.T) {
	_, err := renderTemplate(`{{#if x}}stuck`, nil, nil, true)
	if err == nil {
		t.Error("expected error for unterminated #if")
	}
	_, err = renderTemplate(`{{#each xs}}body`, nil, nil, true)
	if err == nil {
		t.Error("expected error for unterminated #each")
	}
}

func TestTemplateNestedEachAndIf(t *testing.T) {
	tag1 := NewOrderedMap()
	tag1.Set("name", StringValue("go"))
	tag1.Set("hot", BoolValue(true))
	tag2 := NewOrderedMap()
	tag2.Set("name", StringValue("rust"))
	tag2.Set("hot", BoolValue(false))
	vars := NewOrderedMap()
	vars.Set("tags", ArrayValue([]Value{ObjectValue(tag1), ObjectValue(tag2)}))

	tmpl := `{{#each tags}}{{name}}{{#if hot}}!{{/if}} {{/each}}`
	out, _ := renderTemplate(tmpl, vars, nil, true)
	if out != "go! rust " {
		t.Errorf("got %q", out)
	}
}
