package checker

import (
	"strings"
	"testing"

	"github.com/jlkdevelop/mxscript/lexer"
	"github.com/jlkdevelop/mxscript/parser"
)

func parseProg(t *testing.T, src string) *parser.Program {
	t.Helper()
	tokens, err := lexer.New(src).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.New(tokens).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

func errorMessages(diags []Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == SeverityError {
			out = append(out, d.Message)
		}
	}
	return out
}

func TestCheckerCleanProgram(t *testing.T) {
	prog := parseProg(t, `
fn greet(name) {
  return "hi " + name
}
let x = greet("world")
println(x)
`)
	for _, d := range Check(prog) {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
}

func TestCheckerUndefinedIdentifier(t *testing.T) {
	prog := parseProg(t, `
let x = 1
let y = z + x
`)
	got := errorMessages(Check(prog))
	if len(got) == 0 || !strings.Contains(got[0], `undefined identifier "z"`) {
		t.Errorf("expected undefined-identifier error, got %v", got)
	}
}

func TestCheckerWrongArity(t *testing.T) {
	prog := parseProg(t, `
fn add(a, b) { return a + b }
let r = add(1)
`)
	got := errorMessages(Check(prog))
	if len(got) == 0 || !strings.Contains(got[0], "expects 2 argument(s), got 1") {
		t.Errorf("expected arity error, got %v", got)
	}
}

func TestCheckerArityCorrectIsClean(t *testing.T) {
	prog := parseProg(t, `
fn add(a, b) { return a + b }
let r = add(1, 2)
`)
	if errs := errorMessages(Check(prog)); len(errs) > 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestCheckerSpreadCallSkipsArity(t *testing.T) {
	prog := parseProg(t, `
fn add(a, b) { return a + b }
let xs = [1, 2]
let r = add(...xs)
`)
	if errs := errorMessages(Check(prog)); len(errs) > 0 {
		t.Errorf("spread call should not flag arity, got %v", errs)
	}
}

func TestCheckerBuiltinsAreKnown(t *testing.T) {
	prog := parseProg(t, `
let xs = [1, 2, 3]
let total = 0
loop xs as n {
  total = total + n
}
println(json_stringify({ total: total }))
`)
	if errs := errorMessages(Check(prog)); len(errs) > 0 {
		t.Errorf("builtins should be known, got %v", errs)
	}
}

func TestCheckerRouteHasRequest(t *testing.T) {
	prog := parseProg(t, `
route GET /hello {
  return json({ name: request.params.name })
}
`)
	if errs := errorMessages(Check(prog)); len(errs) > 0 {
		t.Errorf("request should be auto-bound in route bodies, got %v", errs)
	}
}

func TestCheckerLoopVarScoped(t *testing.T) {
	prog := parseProg(t, `
let xs = [1, 2, 3]
loop xs as n {
  println(n)
}
`)
	if errs := errorMessages(Check(prog)); len(errs) > 0 {
		t.Errorf("loop var should be in scope, got %v", errs)
	}
}

func TestCheckerNestedScope(t *testing.T) {
	// Inner declaration leaking outward must NOT be flagged at the
	// outer level: `inner` is undefined at the top level.
	prog := parseProg(t, `
fn outer() {
  let inner = 5
  return inner
}
let bad = inner
`)
	got := errorMessages(Check(prog))
	found := false
	for _, m := range got {
		if strings.Contains(m, `undefined identifier "inner"`) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected undefined-identifier for outer-scoped use of inner, got %v", got)
	}
}

func TestCheckerMutualRecursion(t *testing.T) {
	// fn declarations are forward-declared so a calls b before b is
	// defined and vice versa — both must be valid.
	prog := parseProg(t, `
fn a(n) {
  if n <= 0 { return 0 }
  return b(n - 1)
}
fn b(n) {
  if n <= 0 { return 1 }
  return a(n - 1)
}
let r = a(5)
`)
	if errs := errorMessages(Check(prog)); len(errs) > 0 {
		t.Errorf("mutual recursion should be clean, got %v", errs)
	}
}

func TestCheckerUnusedWarning(t *testing.T) {
	prog := parseProg(t, `let unused = 42`)
	diags := Check(prog)
	found := false
	for _, d := range diags {
		if d.Severity == SeverityWarning && strings.Contains(d.Message, "unused") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected unused-let warning, got %+v", diags)
	}
}

func TestCheckerUnderscoredVarSkipsUnused(t *testing.T) {
	prog := parseProg(t, `let _ignored = 42`)
	for _, d := range Check(prog) {
		if d.Severity == SeverityWarning {
			t.Errorf("underscored binding should not warn: %s", d.Message)
		}
	}
}

func TestCheckerDestructureBindings(t *testing.T) {
	prog := parseProg(t, `
let user = { name: "Jassim", age: 30 }
let { name, age } = user
println(name, age)
`)
	if errs := errorMessages(Check(prog)); len(errs) > 0 {
		t.Errorf("destructure bindings should be in scope, got %v", errs)
	}
}

func TestCheckerImportNamespace(t *testing.T) {
	// Namespaced import should bind the namespace identifier so
	// `auth.login()` doesn't flag `auth` as undefined.
	prog := parseProg(t, `
import "./auth.mx" as auth
println(auth)
`)
	for _, d := range Check(prog) {
		if d.Severity == SeverityError {
			t.Errorf("namespaced import should bind: %s", d.Message)
		}
	}
}
