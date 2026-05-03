package interpreter

import (
	"testing"

	"github.com/jlkdevelop/mxscript/lexer"
	"github.com/jlkdevelop/mxscript/parser"
)

// parseExpr lexes and parses a single expression — wrapped in `let _x =`
// so the parser's statement entry point accepts it, then we pull the
// expression back out.
func parseExpr(t *testing.T, src string) parser.Expr {
	t.Helper()
	tokens, err := lexer.New("let _x = " + src).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.New(tokens).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(prog.Stmts) != 1 {
		t.Fatalf("want 1 stmt, got %d", len(prog.Stmts))
	}
	let, ok := prog.Stmts[0].(*parser.LetStmt)
	if !ok {
		t.Fatalf("want LetStmt, got %T", prog.Stmts[0])
	}
	return let.Value
}

func runVM(t *testing.T, src string, env *Env) Value {
	t.Helper()
	c, ok := CompileExpr(parseExpr(t, src))
	if !ok {
		t.Fatalf("compile refused for %q", src)
	}
	v, err := c.Run(nil, env)
	if err != nil {
		t.Fatalf("run %q: %v", src, err)
	}
	return v
}

func TestVMArithmetic(t *testing.T) {
	cases := []struct {
		src  string
		want float64
	}{
		{"1 + 2", 3},
		{"10 - 4", 6},
		{"3 * 7", 21},
		{"20 / 4", 5},
		{"10 % 3", 1},
		{"-5 + 8", 3},
		{"2 + 3 * 4", 14},
		{"(2 + 3) * 4", 20},
	}
	env := NewEnv(nil)
	for _, tc := range cases {
		got := runVM(t, tc.src, env)
		if got.Kind != KindNumber || got.Number != tc.want {
			t.Errorf("%q: want %v, got %+v", tc.src, tc.want, got)
		}
	}
}

func TestVMComparison(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"1 < 2", true},
		{"2 < 1", false},
		{"3 == 3", true},
		{"3 != 4", true},
		{"5 >= 5", true},
		{"5 <= 4", false},
	}
	env := NewEnv(nil)
	for _, tc := range cases {
		got := runVM(t, tc.src, env)
		if got.Kind != KindBool || got.Bool != tc.want {
			t.Errorf("%q: want %v, got %+v", tc.src, tc.want, got)
		}
	}
}

func TestVMIdentifierLoad(t *testing.T) {
	env := NewEnv(nil)
	env.Set("x", NumberValue(42))
	env.Set("y", NumberValue(8))
	got := runVM(t, "x + y", env)
	if got.Kind != KindNumber || got.Number != 50 {
		t.Errorf("x + y: want 50, got %+v", got)
	}
}

func TestVMUndefinedIdent(t *testing.T) {
	env := NewEnv(nil)
	c, ok := CompileExpr(parseExpr(t, "missing + 1"))
	if !ok {
		t.Fatal("compile refused")
	}
	if _, err := c.Run(nil, env); err == nil {
		t.Error("expected undefined-identifier error, got nil")
	}
}

func TestVMRefusesShortCircuit(t *testing.T) {
	// && / || / ?? need branching; the MVP compiler bails out and
	// callers fall back to the tree-walker.
	for _, src := range []string{"true && false", "1 || 2", "null ?? 7"} {
		if _, ok := CompileExpr(parseExpr(t, src)); ok {
			t.Errorf("%q: expected compile to refuse, but it accepted", src)
		}
	}
}

func TestVMRefusesUnsupportedNode(t *testing.T) {
	// Array and object literals still need their own opcodes; the VM
	// compiler bails out for them. Calls now compile (OpCall) so
	// they're no longer in this list.
	for _, src := range []string{"[1,2,3]", "{ a: 1 }"} {
		if _, ok := CompileExpr(parseExpr(t, src)); ok {
			t.Errorf("%q: expected compile to refuse, but it accepted", src)
		}
	}
}

func TestVMDivideByZero(t *testing.T) {
	c, ok := CompileExpr(parseExpr(t, "1 / 0"))
	if !ok {
		t.Fatal("compile refused")
	}
	if _, err := c.Run(nil, NewEnv(nil)); err == nil {
		t.Error("expected division-by-zero error, got nil")
	}
}

func TestVMCompilesLetAndAssign(t *testing.T) {
	src := `let x = 10
x = x + 5`
	tokens, err := lexer.New(src).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.New(tokens).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, ok := CompileBlock(prog.Stmts)
	if !ok {
		t.Fatal("compile refused let+assign")
	}
	env := NewEnv(nil)
	if _, err := c.Run(nil, env); err != nil {
		t.Fatalf("run: %v", err)
	}
	v, ok := env.Get("x")
	if !ok || v.Kind != KindNumber || v.Number != 15 {
		t.Errorf("x: want 15, got %+v", v)
	}
}

func TestVMCompilesIfStatement(t *testing.T) {
	src := `let x = 10
if x > 5 {
  x = 100
} else {
  x = -1
}`
	tokens, _ := lexer.New(src).Tokenize()
	prog, _ := parser.New(tokens).Parse()
	c, ok := CompileBlock(prog.Stmts)
	if !ok {
		t.Fatal("compile refused if")
	}
	env := NewEnv(nil)
	if _, err := c.Run(nil, env); err != nil {
		t.Fatalf("run: %v", err)
	}
	v, _ := env.Get("x")
	if v.Number != 100 {
		t.Errorf("x: want 100, got %v", v.Number)
	}
}

func TestVMCompilesWhileLoop(t *testing.T) {
	src := `let total = 0
let i = 0
while i < 100 {
  total = total + i
  i = i + 1
}`
	tokens, _ := lexer.New(src).Tokenize()
	prog, _ := parser.New(tokens).Parse()
	c, ok := CompileBlock(prog.Stmts)
	if !ok {
		t.Fatal("compile refused while")
	}
	env := NewEnv(nil)
	if _, err := c.Run(nil, env); err != nil {
		t.Fatalf("run: %v", err)
	}
	v, _ := env.Get("total")
	if v.Number != 4950 { // 0+1+...+99
		t.Errorf("total: want 4950, got %v", v.Number)
	}
}

func TestVMRefusesUnsupportedStmt(t *testing.T) {
	// loop / try / destructuring lets still fall back. return now
	// compiles since the VM has OpReturn for function-body lowering.
	cases := []string{
		`let { a, b } = { a: 1, b: 2 }`, // destructuring
		`try { 1 } catch e { 2 }`,       // try
		`loop [1,2,3] as x { }`,         // loop
	}
	for _, src := range cases {
		tokens, err := lexer.New(src).Tokenize()
		if err != nil {
			continue
		}
		prog, err := parser.New(tokens).Parse()
		if err != nil {
			continue
		}
		if _, ok := CompileBlock(prog.Stmts); ok {
			t.Errorf("%q: expected compile refusal", src)
		}
	}
}

func TestVMCompilesCallExpression(t *testing.T) {
	src := `
fn double(n) { return n * 2 }
let x = double(21)
`
	tokens, _ := lexer.New(src).Tokenize()
	prog, _ := parser.New(tokens).Parse()
	i := New()
	i.SetBytecode(true)
	if _, err := i.Exec(prog); err != nil {
		t.Fatalf("exec: %v", err)
	}
	v, _ := i.Globals().Get("x")
	if v.Kind != KindNumber || v.Number != 42 {
		t.Errorf("x: want 42, got %+v", v)
	}
}

func TestVMCompilesReturnStatement(t *testing.T) {
	src := `
fn pick(n) {
  if n > 5 { return "big" }
  return "small"
}
let a = pick(10)
let b = pick(2)
`
	tokens, _ := lexer.New(src).Tokenize()
	prog, _ := parser.New(tokens).Parse()
	i := New()
	i.SetBytecode(true)
	if _, err := i.Exec(prog); err != nil {
		t.Fatalf("exec: %v", err)
	}
	a, _ := i.Globals().Get("a")
	b, _ := i.Globals().Get("b")
	if a.String != "big" || b.String != "small" {
		t.Errorf("a=%v b=%v", a, b)
	}
}

func TestVMCompilesMemberAccess(t *testing.T) {
	src := `
let user = { name: "Jassim", age: 30 }
let n = user.name
`
	tokens, _ := lexer.New(src).Tokenize()
	prog, _ := parser.New(tokens).Parse()
	i := New()
	i.SetBytecode(true)
	if _, err := i.Exec(prog); err != nil {
		t.Fatalf("exec: %v", err)
	}
	v, _ := i.Globals().Get("n")
	if v.String != "Jassim" {
		t.Errorf("n: got %v", v)
	}
}

func TestVMFunctionBodyCachedAfterFirstCall(t *testing.T) {
	// The compiledBody cache should make the second call use the
	// same bytecode object as the first; we can't observe that
	// directly without exposing internals, but we CAN observe that
	// repeated calls produce identical results without errors —
	// which means cache lookup + run + result stays consistent.
	src := `
fn add(a, b) { return a + b }
`
	tokens, _ := lexer.New(src).Tokenize()
	prog, _ := parser.New(tokens).Parse()
	i := New()
	i.SetBytecode(true)
	if _, err := i.Exec(prog); err != nil {
		t.Fatalf("exec: %v", err)
	}
	for k := 0; k < 5; k++ {
		v, err := i.CallByName("add", []Value{NumberValue(2), NumberValue(3)})
		if err != nil {
			t.Fatalf("call %d: %v", k, err)
		}
		if v.Number != 5 {
			t.Errorf("call %d: want 5, got %v", k, v.Number)
		}
	}
}

func TestInterpreterBytecodeFlag(t *testing.T) {
	// End-to-end: enabling the flag must not change observable behaviour
	// for programs the VM lowers. Run the same expression through both
	// engines and confirm the result matches.
	src := `let a = 10
let b = 3
let c = a * b + 5`

	for _, useVM := range []bool{false, true} {
		tokens, err := lexer.New(src).Tokenize()
		if err != nil {
			t.Fatalf("lex: %v", err)
		}
		prog, err := parser.New(tokens).Parse()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		i := New()
		i.SetBytecode(useVM)
		if _, err := i.Exec(prog); err != nil {
			t.Fatalf("exec (vm=%v): %v", useVM, err)
		}
		v, ok := i.Globals().Get("c")
		if !ok || v.Kind != KindNumber || v.Number != 35 {
			t.Errorf("vm=%v: want c=35, got %+v", useVM, v)
		}
	}
}
