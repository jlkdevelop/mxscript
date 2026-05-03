// checker is the static-analysis pass behind `mx check`. Today it
// catches three classes of bug before the program runs:
//
//  1. undefined identifiers (typos, forgotten imports)
//  2. wrong arity on calls to user-defined functions
//  3. unused `let` bindings (warnings, not errors)
//
// The checker walks the AST exactly once, maintaining a scope chain
// that mirrors how the interpreter would build environments at run
// time. When it visits a route or function body, it pushes a new
// child scope so locals don't leak into siblings.
//
// The checker doesn't attempt to model values or types — MX is
// dynamically typed and most useful errors at this layer are about
// names, not types. A real type system can grow on top later.
package checker

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jlkdevelop/mxscript/interpreter"
	"github.com/jlkdevelop/mxscript/parser"
)

// Severity controls whether a diagnostic stops the build.
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)

func (s Severity) String() string {
	if s == SeverityWarning {
		return "warning"
	}
	return "error"
}

// Diagnostic is one issue found during analysis.
type Diagnostic struct {
	Severity Severity
	Line     int
	Col      int
	Message  string
}

// Check runs the analyzer over a parsed program and returns every
// diagnostic found, in source order.
func Check(prog *parser.Program) []Diagnostic {
	c := &checker{
		userFns: map[string]int{},
	}
	c.pushScope()
	c.collectTopLevel(prog)
	for _, s := range prog.Stmts {
		c.checkStmt(s)
	}
	c.flagUnused()
	c.popScope()

	sort.Slice(c.diags, func(i, j int) bool {
		if c.diags[i].Line != c.diags[j].Line {
			return c.diags[i].Line < c.diags[j].Line
		}
		return c.diags[i].Col < c.diags[j].Col
	})
	return c.diags
}

type binding struct {
	used bool
	line int
	col  int
}

type checker struct {
	scopes  []map[string]*binding
	diags   []Diagnostic
	userFns map[string]int // name -> arity. Lets us check call counts.
}

func (c *checker) pushScope() {
	c.scopes = append(c.scopes, map[string]*binding{})
}

func (c *checker) popScope() {
	c.scopes = c.scopes[:len(c.scopes)-1]
}

func (c *checker) declare(name string, line, col int) {
	// Underscored names are intentionally unused; don't track them.
	if strings.HasPrefix(name, "_") {
		return
	}
	c.scopes[len(c.scopes)-1][name] = &binding{line: line, col: col}
}

func (c *checker) markUsed(name string) bool {
	for k := len(c.scopes) - 1; k >= 0; k-- {
		if b, ok := c.scopes[k][name]; ok {
			b.used = true
			return true
		}
	}
	return false
}

func (c *checker) addDiag(severity Severity, line, col int, format string, args ...any) {
	c.diags = append(c.diags, Diagnostic{
		Severity: severity,
		Line:     line,
		Col:      col,
		Message:  fmt.Sprintf(format, args...),
	})
}

// collectTopLevel forward-declares every top-level fn and let so
// programs with mutual references compile without false positives.
// (Tree-walker hoists fn declarations the same way at runtime.)
func (c *checker) collectTopLevel(prog *parser.Program) {
	for _, s := range prog.Stmts {
		switch n := s.(type) {
		case *parser.FnDecl:
			c.declare(n.Name, 0, 0)
			c.userFns[n.Name] = len(n.Params)
		case *parser.LetStmt:
			if n.Name != "" {
				c.declare(n.Name, 0, 0)
			}
			if n.Pattern != nil {
				c.declarePattern(n.Pattern)
			}
		case *parser.MiddlewareDecl:
			c.declare(n.Name, 0, 0)
		case *parser.ImportStmt:
			if n.As != "" {
				c.declare(n.As, 0, 0)
			}
		}
	}
}

func (c *checker) declarePattern(p *parser.DestructurePattern) {
	for _, b := range p.Items {
		c.declare(b.Name, 0, 0)
	}
}

// flagUnused walks the current (top-level) scope and warns about
// `let` bindings that were never read. Skips fn declarations since
// "unused" is the wrong word — it just means nobody calls them yet,
// which is fine for routes / handlers / library exports.
func (c *checker) flagUnused() {
	for name, b := range c.scopes[0] {
		if !b.used && b.line > 0 {
			c.addDiag(SeverityWarning, b.line, b.col, "unused let binding %q", name)
		}
	}
}

// ===== Statement walking =====

func (c *checker) checkStmt(s parser.Stmt) {
	switch n := s.(type) {
	case *parser.LetStmt:
		c.checkExpr(n.Value)
		line, col := n.Pos()
		if n.Name != "" {
			// Only record location for let bindings declared in inner
			// scopes — top-level ones are already in the scope from
			// collectTopLevel and we'd lose their position info.
			if len(c.scopes) > 1 {
				c.declare(n.Name, line, col)
			} else {
				if b, ok := c.scopes[0][n.Name]; ok {
					b.line = line
					b.col = col
				}
			}
		}
		if n.Pattern != nil {
			c.declarePattern(n.Pattern)
		}
	case *parser.AssignStmt:
		c.checkExpr(n.Value)
		c.checkExpr(n.Target)
	case *parser.FnDecl:
		// Declare the function in the enclosing scope so subsequent
		// statements (and the function's own body, for recursion) can
		// reference it. Top-level fn decls are already covered by
		// collectTopLevel; this branch handles nested ones inside
		// function bodies, route handlers, loop bodies, etc.
		if len(c.scopes) > 1 {
			c.declare(n.Name, 0, 0)
			// Track arity for nested user-defined functions too —
			// otherwise calls to them would silently skip checking.
			c.userFns[n.Name] = len(n.Params)
		}
		c.checkFnBody(n.Params, n.Body)
	case *parser.RouteDecl:
		c.checkRouteBody(n.Method, n.Body)
	case *parser.GroupStmt:
		// Group bodies see the same scope but with shared middleware;
		// we don't model the middleware semantics here.
		for _, inner := range n.Body {
			c.checkStmt(inner)
		}
	case *parser.MiddlewareDecl:
		c.checkRouteBody("", n.Body)
	case *parser.TestDecl:
		// A test body is just a fresh scope with the file's globals
		// in scope — same shape as a function body with no params.
		c.checkFnBody(nil, n.Body)
	case *parser.BenchDecl:
		c.checkFnBody(nil, n.Body)
	case *parser.UseStmt:
		// Verify the middleware exists.
		if !c.markUsed(n.Name) && !interpreter.IsBuiltin(n.Name) {
			line, col := n.Pos()
			c.addDiag(SeverityError, line, col, "use of undefined middleware %q", n.Name)
		}
	case *parser.IfStmt:
		c.checkExpr(n.Cond)
		c.pushScope()
		for _, inner := range n.Then {
			c.checkStmt(inner)
		}
		c.popScope()
		c.pushScope()
		for _, inner := range n.Else {
			c.checkStmt(inner)
		}
		c.popScope()
	case *parser.LoopStmt:
		c.checkExpr(n.Iterable)
		c.pushScope()
		c.declare(n.Var, 0, 0)
		if n.IndexVar != "" {
			c.declare(n.IndexVar, 0, 0)
		}
		for _, inner := range n.Body {
			c.checkStmt(inner)
		}
		c.popScope()
	case *parser.WhileStmt:
		c.checkExpr(n.Cond)
		c.pushScope()
		for _, inner := range n.Body {
			c.checkStmt(inner)
		}
		c.popScope()
	case *parser.TryStmt:
		c.pushScope()
		for _, inner := range n.Try {
			c.checkStmt(inner)
		}
		c.popScope()
		c.pushScope()
		if n.CatchVar != "" {
			c.declare(n.CatchVar, 0, 0)
		}
		for _, inner := range n.Catch {
			c.checkStmt(inner)
		}
		c.popScope()
	case *parser.ReturnStmt:
		if n.Value != nil {
			c.checkExpr(n.Value)
		}
	case *parser.ExprStmt:
		c.checkExpr(n.Expr)
	case *parser.SpawnStmt:
		c.pushScope()
		for _, inner := range n.Body {
			c.checkStmt(inner)
		}
		c.popScope()
	}
}

// checkFnBody enters a new scope, declares each parameter, and walks
// the body. Top-level identifiers declared by the body's `let` stay
// inside the function's scope.
func (c *checker) checkFnBody(params []string, body []parser.Stmt) {
	c.pushScope()
	for _, p := range params {
		c.declare(p, 0, 0)
	}
	for _, s := range body {
		c.checkStmt(s)
	}
	c.popScope()
}

// checkRouteBody is like checkFnBody but pre-declares `request` plus
// any method-specific context bindings (WS routes get send/recv/close,
// SSE routes get send) so handlers don't false-positive on the names
// the runtime injects.
func (c *checker) checkRouteBody(method string, body []parser.Stmt) {
	c.pushScope()
	c.declare("request", 0, 0)
	switch method {
	case "WS":
		c.declare("send", 0, 0)
		c.declare("recv", 0, 0)
		c.declare("close", 0, 0)
	case "SSE":
		c.declare("send", 0, 0)
	}
	for _, s := range body {
		c.checkStmt(s)
	}
	c.popScope()
}

// ===== Expression walking =====

func (c *checker) checkExpr(e parser.Expr) {
	switch n := e.(type) {
	case *parser.Identifier:
		if !c.markUsed(n.Name) && !interpreter.IsBuiltin(n.Name) {
			line, col := n.Pos()
			c.addDiag(SeverityError, line, col, "undefined identifier %q", n.Name)
		}
	case *parser.BinaryExpr:
		c.checkExpr(n.Left)
		c.checkExpr(n.Right)
	case *parser.RangeExpr:
		c.checkExpr(n.Start)
		c.checkExpr(n.End)
	case *parser.UnaryExpr:
		c.checkExpr(n.Operand)
	case *parser.CallExpr:
		c.checkExpr(n.Callee)
		for _, a := range n.Args {
			c.checkExpr(a)
		}
		c.checkArity(n)
	case *parser.IndexExpr:
		c.checkExpr(n.Object)
		c.checkExpr(n.Index)
	case *parser.MemberExpr:
		c.checkExpr(n.Object)
	case *parser.ArrayLit:
		for _, el := range n.Elements {
			c.checkExpr(el)
		}
	case *parser.ObjectLit:
		for _, p := range n.Pairs {
			c.checkExpr(p.Value)
		}
	case *parser.FnLit:
		c.checkFnBody(n.Params, n.Body)
	case *parser.MatchExpr:
		c.checkExpr(n.Subject)
		for _, arm := range n.Arms {
			if arm.Pattern != nil {
				c.checkExpr(arm.Pattern)
			}
			c.checkExpr(arm.Body)
		}
	case *parser.TryExpr:
		for _, s := range n.Try {
			c.checkStmt(s)
		}
		for _, s := range n.Catch {
			c.checkStmt(s)
		}
	case *parser.SpreadExpr:
		c.checkExpr(n.Inner)
	}
}

// checkArity flags calls to user-defined functions whose argument
// count doesn't match the declaration. We don't check builtins
// because their signatures aren't structured (different builtins
// accept different shapes — arrays, options objects, varargs).
func (c *checker) checkArity(call *parser.CallExpr) {
	id, ok := call.Callee.(*parser.Identifier)
	if !ok {
		return
	}
	want, ok := c.userFns[id.Name]
	if !ok {
		return
	}
	got := len(call.Args)
	// Spread arguments could pass any number of values; skip the check.
	for _, a := range call.Args {
		if _, isSpread := a.(*parser.SpreadExpr); isSpread {
			return
		}
	}
	if got != want {
		line, col := call.Pos()
		c.addDiag(SeverityError, line, col,
			"function %q expects %d argument(s), got %d", id.Name, want, got)
	}
}
