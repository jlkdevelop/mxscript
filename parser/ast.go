// Package parser defines the AST node types for MX Script and the
// recursive-descent parser that builds an AST from a token stream.
package parser

// Node is the base type for every AST node.
type Node interface {
	Pos() (line, col int)
}

// Expr is the type implemented by all expression nodes.
type Expr interface {
	Node
	exprNode()
}

// Stmt is the type implemented by all statement / top-level declaration nodes.
type Stmt interface {
	Node
	stmtNode()
}

type pos struct{ Line, Col int }

func (p pos) Pos() (int, int) { return p.Line, p.Col }

// ===== Top-level =====

type Program struct {
	Stmts []Stmt
}

func (p *Program) Pos() (int, int) {
	if len(p.Stmts) > 0 {
		return p.Stmts[0].Pos()
	}
	return 1, 1
}

// ===== Statements =====

type LetStmt struct {
	pos
	Name  string
	Value Expr
}

func (*LetStmt) stmtNode() {}

type AssignStmt struct {
	pos
	Target Expr
	Value  Expr
}

func (*AssignStmt) stmtNode() {}

type FnDecl struct {
	pos
	Name   string
	Params []string
	Body   []Stmt
}

func (*FnDecl) stmtNode() {}

type ServerBlock struct {
	pos
	Settings []ObjectPair
}

func (*ServerBlock) stmtNode() {}

type RouteDecl struct {
	pos
	Method string
	Path   string
	Body   []Stmt
}

func (*RouteDecl) stmtNode() {}

type MiddlewareDecl struct {
	pos
	Name   string
	Params []string
	Body   []Stmt
}

func (*MiddlewareDecl) stmtNode() {}

// UseStmt attaches a named middleware inside a route or globally.
type UseStmt struct {
	pos
	Name string
}

func (*UseStmt) stmtNode() {}

type IfStmt struct {
	pos
	Cond Expr
	Then []Stmt
	Else []Stmt
}

func (*IfStmt) stmtNode() {}

// LoopStmt is `loop iterable as item { ... }` — iterates arrays / numeric ranges.
type LoopStmt struct {
	pos
	Iterable Expr
	Var      string
	Body     []Stmt
}

func (*LoopStmt) stmtNode() {}

// WhileStmt is `while (cond) { ... }`.
type WhileStmt struct {
	pos
	Cond Expr
	Body []Stmt
}

func (*WhileStmt) stmtNode() {}

// BreakStmt exits the nearest enclosing loop.
type BreakStmt struct{ pos }

func (*BreakStmt) stmtNode() {}

// ContinueStmt jumps to the next iteration of the nearest enclosing loop.
type ContinueStmt struct{ pos }

func (*ContinueStmt) stmtNode() {}

type TryStmt struct {
	pos
	Try      []Stmt
	CatchVar string
	Catch    []Stmt
}

func (*TryStmt) stmtNode() {}

type ReturnStmt struct {
	pos
	Value Expr // nil if `return` with no value
}

func (*ReturnStmt) stmtNode() {}

type ExprStmt struct {
	pos
	Expr Expr
}

func (*ExprStmt) stmtNode() {}

type ImportStmt struct {
	pos
	Path string
}

func (*ImportStmt) stmtNode() {}

// ===== Expressions =====

type NumberLit struct {
	pos
	Value float64
}

func (*NumberLit) exprNode() {}

type StringLit struct {
	pos
	Value string
}

func (*StringLit) exprNode() {}

type BoolLit struct {
	pos
	Value bool
}

func (*BoolLit) exprNode() {}

type NullLit struct{ pos }

func (*NullLit) exprNode() {}

type Identifier struct {
	pos
	Name string
}

func (*Identifier) exprNode() {}

type ArrayLit struct {
	pos
	Elements []Expr
}

func (*ArrayLit) exprNode() {}

type ObjectPair struct {
	Key   string
	Value Expr
}

type ObjectLit struct {
	pos
	Pairs []ObjectPair
}

func (*ObjectLit) exprNode() {}

type BinaryExpr struct {
	pos
	Op    string
	Left  Expr
	Right Expr
}

func (*BinaryExpr) exprNode() {}

type UnaryExpr struct {
	pos
	Op      string
	Operand Expr
}

func (*UnaryExpr) exprNode() {}

type CallExpr struct {
	pos
	Callee Expr
	Args   []Expr
}

func (*CallExpr) exprNode() {}

type IndexExpr struct {
	pos
	Object Expr
	Index  Expr
}

func (*IndexExpr) exprNode() {}

type MemberExpr struct {
	pos
	Object   Expr
	Property string
}

func (*MemberExpr) exprNode() {}

// FnLit is an anonymous function literal: `fn(x, y) { ... }`.
type FnLit struct {
	pos
	Params []string
	Body   []Stmt
}

func (*FnLit) exprNode() {}
