// visit.go — utilities for walking an AST. Used by tooling (formatter,
// coverage reporter, future LSP enhancements) that need to enumerate
// every statement / expression position.
package parser

// ExecutableLines walks the program and returns the set of source-line
// numbers that carry an executable statement. The coverage reporter
// compares this against the lines that actually ran.
func ExecutableLines(prog *Program) map[int]bool {
	out := map[int]bool{}
	var walk func(s Stmt)
	walk = func(s Stmt) {
		if s == nil {
			return
		}
		line, _ := s.Pos()
		out[line] = true
		switch n := s.(type) {
		case *FnDecl:
			for _, b := range n.Body {
				walk(b)
			}
		case *MiddlewareDecl:
			for _, b := range n.Body {
				walk(b)
			}
		case *RouteDecl:
			for _, b := range n.Body {
				walk(b)
			}
		case *IfStmt:
			for _, b := range n.Then {
				walk(b)
			}
			for _, b := range n.Else {
				walk(b)
			}
		case *LoopStmt:
			for _, b := range n.Body {
				walk(b)
			}
		case *WhileStmt:
			for _, b := range n.Body {
				walk(b)
			}
		case *TryStmt:
			for _, b := range n.Try {
				walk(b)
			}
			for _, b := range n.Catch {
				walk(b)
			}
		case *SpawnStmt:
			for _, b := range n.Body {
				walk(b)
			}
		}
	}
	for _, s := range prog.Stmts {
		walk(s)
	}
	return out
}
