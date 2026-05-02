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

// LoopStmt is `loop iterable as item { ... }` or `loop iterable as i, item { ... }`.
// IndexVar is the empty string when no index is requested.
type LoopStmt struct {
	pos
	Iterable Expr
	IndexVar string
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

// SpawnStmt runs a block in a fresh goroutine. The body shares the
// enclosing closure (read-only by convention — writes from goroutines
// race with the main interpreter and other spawns). Use channels for
// inter-goroutine communication.
type SpawnStmt struct {
	pos
	Body []Stmt
}

func (*SpawnStmt) stmtNode() {}

// StaticStmt declares a static-file mount point.
//
//	static "./public"            // serves files from ./public at /
//	static "./assets" at "/cdn"  // serves files from ./assets at /cdn
type StaticStmt struct {
	pos
	Dir   string
	Mount string // URL prefix; defaults to "/" if not specified
}

func (*StaticStmt) stmtNode() {}

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
	Optional bool // `?.` short-circuits if Object is null
}

func (*MemberExpr) exprNode() {}

// FnLit is an anonymous function literal: `fn(x, y) { ... }`.
type FnLit struct {
	pos
	Params []string
	Body   []Stmt
}

func (*FnLit) exprNode() {}

// SpreadExpr wraps an expression that should be expanded inline inside
// an array literal, an object literal, or a call argument list.
type SpreadExpr struct {
	pos
	Inner Expr
}

func (*SpreadExpr) exprNode() {}

// MatchExpr is `match subject { p1 => e1, p2 => e2, _ => e3 }`. Arms are
// tested top-to-bottom; the first matching arm's body is the result. A
// pattern is either an expression (compared with `==`) or the bare
// identifier `_` which matches anything.
type MatchExpr struct {
	pos
	Subject Expr
	Arms    []MatchArm
}

func (*MatchExpr) exprNode() {}

// TryExpr is the expression form of try/catch. The value of the last
// expression statement in whichever block ran becomes the result.
//
//	let parsed = try { json_parse(input) } catch (e) { { error: e.message } }
type TryExpr struct {
	pos
	Try      []Stmt
	CatchVar string
	Catch    []Stmt
}

func (*TryExpr) exprNode() {}

type MatchArm struct {
	Pattern Expr // nil means wildcard `_`
	Body    Expr
}
