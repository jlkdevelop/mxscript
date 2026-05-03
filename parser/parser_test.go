// parser_test.go covers AST shapes for the most common MX Script constructs.
package parser

import (
	"testing"

	"github.com/jlkdevelop/mxscript/lexer"
)

func mustParse(t *testing.T, src string) *Program {
	t.Helper()
	toks, err := lexer.New(src).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := New(toks).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

func TestParseLet(t *testing.T) {
	prog := mustParse(t, `let x = 42`)
	if len(prog.Stmts) != 1 {
		t.Fatalf("want 1 stmt, got %d", len(prog.Stmts))
	}
	let, ok := prog.Stmts[0].(*LetStmt)
	if !ok {
		t.Fatalf("expected *LetStmt, got %T", prog.Stmts[0])
	}
	if let.Name != "x" {
		t.Errorf("got name %q, want x", let.Name)
	}
	num, ok := let.Value.(*NumberLit)
	if !ok || num.Value != 42 {
		t.Errorf("expected NumberLit(42), got %T %+v", let.Value, let.Value)
	}
}

func TestParseFn(t *testing.T) {
	prog := mustParse(t, `fn add(a, b) { return a + b }`)
	fn, ok := prog.Stmts[0].(*FnDecl)
	if !ok {
		t.Fatalf("expected *FnDecl, got %T", prog.Stmts[0])
	}
	if len(fn.Params) != 2 || fn.Params[0] != "a" || fn.Params[1] != "b" {
		t.Errorf("params: got %v", fn.Params)
	}
	if len(fn.Body) != 1 {
		t.Fatalf("expected 1 body stmt, got %d", len(fn.Body))
	}
}

func TestParseRoute(t *testing.T) {
	prog := mustParse(t, `route GET /users/:id { return json({}) }`)
	rt, ok := prog.Stmts[0].(*RouteDecl)
	if !ok {
		t.Fatalf("expected *RouteDecl, got %T", prog.Stmts[0])
	}
	if rt.Method != "GET" {
		t.Errorf("method: got %q", rt.Method)
	}
	if rt.Path != "/users/:id" {
		t.Errorf("path: got %q", rt.Path)
	}
}

func TestParseIfElse(t *testing.T) {
	prog := mustParse(t, `if (x > 5) { print("big") } else { print("small") }`)
	if _, ok := prog.Stmts[0].(*IfStmt); !ok {
		t.Fatalf("expected *IfStmt, got %T", prog.Stmts[0])
	}
}

func TestParseLoop(t *testing.T) {
	prog := mustParse(t, `loop [1,2,3] as n { print(n) }`)
	lp, ok := prog.Stmts[0].(*LoopStmt)
	if !ok {
		t.Fatalf("expected *LoopStmt, got %T", prog.Stmts[0])
	}
	if lp.Var != "n" {
		t.Errorf("var: got %q", lp.Var)
	}
}

func TestParseObjectLit(t *testing.T) {
	prog := mustParse(t, `let u = { id: 1, name: "x" }`)
	let := prog.Stmts[0].(*LetStmt)
	obj, ok := let.Value.(*ObjectLit)
	if !ok {
		t.Fatalf("expected *ObjectLit, got %T", let.Value)
	}
	if len(obj.Pairs) != 2 {
		t.Errorf("pairs: got %d, want 2", len(obj.Pairs))
	}
	if obj.Pairs[0].Key != "id" || obj.Pairs[1].Key != "name" {
		t.Errorf("keys: got %v", obj.Pairs)
	}
}

func TestParseAnonymousFn(t *testing.T) {
	prog := mustParse(t, `let f = fn(x) { return x }`)
	let := prog.Stmts[0].(*LetStmt)
	if _, ok := let.Value.(*FnLit); !ok {
		t.Fatalf("expected *FnLit, got %T", let.Value)
	}
}

func TestParseError(t *testing.T) {
	toks, _ := lexer.New(`let = 5`).Tokenize()
	if _, err := New(toks).Parse(); err == nil {
		t.Error("expected parse error for `let = 5`, got nil")
	}
}

func TestParseTestDecl(t *testing.T) {
	prog := mustParse(t, `test "addition works" { assert(1 + 1 == 2) }`)
	td, ok := prog.Stmts[0].(*TestDecl)
	if !ok {
		t.Fatalf("expected *TestDecl, got %T", prog.Stmts[0])
	}
	if td.Name != "addition works" {
		t.Errorf("name: got %q", td.Name)
	}
	if len(td.Body) != 1 {
		t.Errorf("body: got %d stmts", len(td.Body))
	}
}

func TestTestIdentifierStillCallable(t *testing.T) {
	// `test(...)` and `test.foo` must still parse as expressions —
	// only `test "literal"` triggers the inline-test form.
	prog := mustParse(t, `let x = test(1, 2)`)
	if _, ok := prog.Stmts[0].(*LetStmt); !ok {
		t.Fatalf("expected *LetStmt, got %T", prog.Stmts[0])
	}
}

func TestPipeRewriteIntoCall(t *testing.T) {
	// `5 |> double` should desugar to a CallExpr with double as
	// callee and 5 as the lone argument.
	prog := mustParse(t, `let x = 5 |> double`)
	let := prog.Stmts[0].(*LetStmt)
	call, ok := let.Value.(*CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", let.Value)
	}
	if id, _ := call.Callee.(*Identifier); id == nil || id.Name != "double" {
		t.Errorf("callee: %+v", call.Callee)
	}
	if len(call.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(call.Args))
	}
}

func TestPipePrependsToExistingArgs(t *testing.T) {
	// `5 |> add(10)` should desugar to add(5, 10) — LHS prepended.
	prog := mustParse(t, `let x = 5 |> add(10)`)
	let := prog.Stmts[0].(*LetStmt)
	call := let.Value.(*CallExpr)
	if len(call.Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(call.Args))
	}
	first, _ := call.Args[0].(*NumberLit)
	if first == nil || first.Value != 5 {
		t.Errorf("first arg should be 5, got %+v", call.Args[0])
	}
}
