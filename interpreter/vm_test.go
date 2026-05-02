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
	v, err := c.Run(env)
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
	if _, err := c.Run(env); err == nil {
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
	// Object/array/call aren't lowered yet — they should fall back.
	for _, src := range []string{"[1,2,3]", "{ a: 1 }", "len(\"hi\")"} {
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
	if _, err := c.Run(NewEnv(nil)); err == nil {
		t.Error("expected division-by-zero error, got nil")
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
