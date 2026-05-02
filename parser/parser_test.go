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
