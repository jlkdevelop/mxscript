// Package parser implements a recursive-descent parser that converts a
// stream of tokens (produced by the lexer) into an MX Script AST.
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jlkdevelop/mxscript/lexer"
)

type Parser struct {
	tokens []lexer.Token
	pos    int
}

func New(tokens []lexer.Token) *Parser {
	return &Parser{tokens: tokens}
}

// Parse consumes the full token stream and returns a Program AST. Any
// syntax error is returned with file-relative line/column information.
func (p *Parser) Parse() (*Program, error) {
	prog := &Program{}
	for !p.isAtEnd() {
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			prog.Stmts = append(prog.Stmts, stmt)
		}
	}
	return prog, nil
}

// ===== Token helpers =====

func (p *Parser) cur() lexer.Token { return p.tokens[p.pos] }
func (p *Parser) peek(n int) lexer.Token {
	if p.pos+n >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.pos+n]
}

func (p *Parser) isAtEnd() bool { return p.cur().Type == lexer.TokenEOF }

func (p *Parser) advance() lexer.Token {
	t := p.tokens[p.pos]
	if !p.isAtEnd() {
		p.pos++
	}
	return t
}

func (p *Parser) check(tt lexer.TokenType) bool {
	return p.cur().Type == tt
}

func (p *Parser) match(types ...lexer.TokenType) bool {
	for _, tt := range types {
		if p.check(tt) {
			p.advance()
			return true
		}
	}
	return false
}

func (p *Parser) expect(tt lexer.TokenType, ctx string) (lexer.Token, error) {
	if p.check(tt) {
		return p.advance(), nil
	}
	return lexer.Token{}, p.errorf("expected %s %s, got %s (%q)", tt, ctx, p.cur().Type, p.cur().Lexeme)
}

// ParseError carries a structured location so the CLI can render
// source-context errors with a caret pointer.
type ParseError struct {
	Line    int
	Col     int
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at line %d col %d: %s", e.Line, e.Col, e.Message)
}

func (p *Parser) errorf(format string, args ...any) error {
	t := p.cur()
	return &ParseError{Line: t.Line, Col: t.Col, Message: fmt.Sprintf(format, args...)}
}

func mkPos(t lexer.Token) pos { return pos{Line: t.Line, Col: t.Col} }

// ===== Statements =====

func (p *Parser) parseStmt() (Stmt, error) {
	switch p.cur().Type {
	case lexer.TokenLet:
		return p.parseLet()
	case lexer.TokenFn:
		return p.parseFn()
	case lexer.TokenServer:
		return p.parseServer()
	case lexer.TokenRoute:
		return p.parseRoute()
	case lexer.TokenMiddleware:
		return p.parseMiddleware()
	case lexer.TokenUse:
		return p.parseUse()
	case lexer.TokenIf:
		return p.parseIf()
	case lexer.TokenLoop:
		return p.parseLoop()
	case lexer.TokenWhile:
		return p.parseWhile()
	case lexer.TokenBreak:
		tok := p.advance()
		p.match(lexer.TokenSemicolon)
		return &BreakStmt{pos: mkPos(tok)}, nil
	case lexer.TokenContinue:
		tok := p.advance()
		p.match(lexer.TokenSemicolon)
		return &ContinueStmt{pos: mkPos(tok)}, nil
	case lexer.TokenTry:
		return p.parseTry()
	case lexer.TokenReturn:
		return p.parseReturn()
	case lexer.TokenImport:
		return p.parseImport()
	case lexer.TokenStatic:
		return p.parseStatic()
	case lexer.TokenSpawn:
		tok := p.advance()
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &SpawnStmt{pos: mkPos(tok), Body: body}, nil
	case lexer.TokenGroup:
		tok := p.advance()
		path, err := p.parsePath()
		if err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &GroupStmt{pos: mkPos(tok), Path: path, Body: body}, nil
	case lexer.TokenSemicolon:
		p.advance()
		return nil, nil
	case lexer.TokenIdent:
		// HTTP method shorthand: `get /users { ... }` is sugar for
		// `route GET /users { ... }`. We only treat it as a route when
		// followed immediately by `/` so user identifiers like `get` and
		// `post` keep working in expression position.
		if isHTTPMethod(p.cur().Lexeme) && p.peek(1).Type == lexer.TokenSlash {
			return p.parseShorthandRoute()
		}
		// Inline test block: `test "name" { ... }`. Disambiguated from
		// `test(...)` calls and `test.foo` member access by requiring a
		// string literal next.
		if p.cur().Lexeme == "test" && p.peek(1).Type == lexer.TokenString {
			return p.parseTest()
		}
		// Same shape for benchmarks: `bench "name" { ... }`.
		if p.cur().Lexeme == "bench" && p.peek(1).Type == lexer.TokenString {
			return p.parseBench()
		}
		return p.parseExprStmt()
	default:
		return p.parseExprStmt()
	}
}

// parseTest is `test "name" { ... }`. The string is the test's
// display name, the block its body. We don't allow expressions for
// the name — keeping it a literal lets `mx test --filter foo` match
// without ever evaluating user code, which matters when a test file
// is full of half-broken work in progress.
func (p *Parser) parseTest() (Stmt, error) {
	tok := p.advance() // `test`
	nameTok, err := p.expect(lexer.TokenString, "as test name (expected a string literal after `test`)")
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &TestDecl{pos: mkPos(tok), Name: nameTok.Lexeme, Body: body}, nil
}

// parseBench is the benchmark counterpart to parseTest: `bench "name"
// { ... }`. Discovered by `mx bench` and run in a calibrated loop.
func (p *Parser) parseBench() (Stmt, error) {
	tok := p.advance() // `bench`
	nameTok, err := p.expect(lexer.TokenString, "as bench name (expected a string literal after `bench`)")
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &BenchDecl{pos: mkPos(tok), Name: nameTok.Lexeme, Body: body}, nil
}

// isHTTPMethod reports whether `name` is one of the HTTP verbs we accept
// as a shorthand route prefix. Case-sensitive — lowercase only, to keep
// the language style consistent. `sse` is server-sent events, `ws` is
// WebSocket — both accepted at the same syntactic position.
func isHTTPMethod(name string) bool {
	switch name {
	case "get", "post", "put", "delete", "patch", "head", "options", "sse", "ws":
		return true
	}
	return false
}

// parseShorthandRoute is the `get /users { ... }` form. Method is taken
// from the leading identifier (uppercased to match RouteDecl semantics).
func (p *Parser) parseShorthandRoute() (Stmt, error) {
	tok := p.advance()
	path, err := p.parsePath()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	method := strings.ToUpper(tok.Lexeme)
	return &RouteDecl{pos: mkPos(tok), Method: method, Path: path, Body: body}, nil
}

func (p *Parser) parseLet() (Stmt, error) {
	tok := p.advance()

	// Destructuring forms: `let { a, b }` or `let [a, b]`.
	if p.check(lexer.TokenLBrace) || p.check(lexer.TokenLBracket) {
		pattern, err := p.parseDestructurePattern()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokenAssign, "after destructure pattern"); err != nil {
			return nil, err
		}
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		p.match(lexer.TokenSemicolon)
		return &LetStmt{pos: mkPos(tok), Pattern: pattern, Value: val}, nil
	}

	name, err := p.expect(lexer.TokenIdent, "after `let`")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokenAssign, "after let name"); err != nil {
		return nil, err
	}
	val, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	p.match(lexer.TokenSemicolon)
	return &LetStmt{pos: mkPos(tok), Name: name.Lexeme, Value: val}, nil
}

// parseDestructurePattern parses object `{ a, b: c, d = "x" }` or array
// `[ a, b = 0, ...rest ]` patterns.
func (p *Parser) parseDestructurePattern() (*DestructurePattern, error) {
	open := p.advance()
	isArray := open.Type == lexer.TokenLBracket
	closeTok := lexer.TokenRBrace
	if isArray {
		closeTok = lexer.TokenRBracket
	}

	var items []DestructureBinding
	if !p.check(closeTok) {
		for {
			b := DestructureBinding{}
			// Array-only: ...rest
			if isArray && p.check(lexer.TokenSpread) {
				p.advance()
				id, err := p.expect(lexer.TokenIdent, "after `...` in destructure")
				if err != nil {
					return nil, err
				}
				b.Name = id.Lexeme
				b.Rest = true
				items = append(items, b)
				break // rest must be last
			}
			id, err := p.expect(lexer.TokenIdent, "as destructure binding")
			if err != nil {
				return nil, err
			}
			b.Name = id.Lexeme
			// Object-only: { name: alias }
			if !isArray && p.check(lexer.TokenColon) {
				p.advance()
				alias, err := p.expect(lexer.TokenIdent, "after `:` in destructure")
				if err != nil {
					return nil, err
				}
				b.Source = b.Name
				b.Name = alias.Lexeme
			}
			// Optional default: `= expr`
			if p.check(lexer.TokenAssign) {
				p.advance()
				def, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				b.Default = def
			}
			items = append(items, b)
			if !p.match(lexer.TokenComma) {
				break
			}
			if p.check(closeTok) {
				break
			}
		}
	}
	if _, err := p.expect(closeTok, "to close destructure pattern"); err != nil {
		return nil, err
	}
	return &DestructurePattern{IsArray: isArray, Items: items}, nil
}

func (p *Parser) parseFn() (Stmt, error) {
	tok := p.advance()
	name, err := p.expect(lexer.TokenIdent, "after `fn`")
	if err != nil {
		return nil, err
	}
	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &FnDecl{pos: mkPos(tok), Name: name.Lexeme, Params: params, Body: body}, nil
}

func (p *Parser) parseParamList() ([]string, error) {
	if _, err := p.expect(lexer.TokenLParen, "to start parameter list"); err != nil {
		return nil, err
	}
	var params []string
	if !p.check(lexer.TokenRParen) {
		for {
			id, err := p.expect(lexer.TokenIdent, "as parameter name")
			if err != nil {
				return nil, err
			}
			params = append(params, id.Lexeme)
			if !p.match(lexer.TokenComma) {
				break
			}
		}
	}
	if _, err := p.expect(lexer.TokenRParen, "to close parameter list"); err != nil {
		return nil, err
	}
	return params, nil
}

func (p *Parser) parseBlock() ([]Stmt, error) {
	if _, err := p.expect(lexer.TokenLBrace, "to start block"); err != nil {
		return nil, err
	}
	var stmts []Stmt
	for !p.check(lexer.TokenRBrace) && !p.isAtEnd() {
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		if s != nil {
			stmts = append(stmts, s)
		}
	}
	if _, err := p.expect(lexer.TokenRBrace, "to close block"); err != nil {
		return nil, err
	}
	return stmts, nil
}

func (p *Parser) parseServer() (Stmt, error) {
	tok := p.advance()
	if _, err := p.expect(lexer.TokenLBrace, "after `server`"); err != nil {
		return nil, err
	}
	var pairs []ObjectPair
	for !p.check(lexer.TokenRBrace) && !p.isAtEnd() {
		key, err := p.expect(lexer.TokenIdent, "as server setting key")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokenColon, "after server setting key"); err != nil {
			return nil, err
		}
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, ObjectPair{Key: key.Lexeme, Value: val})
		p.match(lexer.TokenComma)
		p.match(lexer.TokenSemicolon)
	}
	if _, err := p.expect(lexer.TokenRBrace, "to close server block"); err != nil {
		return nil, err
	}
	return &ServerBlock{pos: mkPos(tok), Settings: pairs}, nil
}

func (p *Parser) parseRoute() (Stmt, error) {
	tok := p.advance()
	method, err := p.expect(lexer.TokenIdent, "as HTTP method")
	if err != nil {
		return nil, err
	}
	path, err := p.parsePath()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &RouteDecl{pos: mkPos(tok), Method: method.Lexeme, Path: path, Body: body}, nil
}

// parsePath consumes tokens that form a route path like `/users/:id`. It
// stops at the first `{`, since that starts the route body.
func (p *Parser) parsePath() (string, error) {
	if !p.check(lexer.TokenSlash) {
		return "", p.errorf("expected `/` to start route path, got %s", p.cur().Type)
	}
	var s string
	for !p.check(lexer.TokenLBrace) && !p.isAtEnd() {
		t := p.cur()
		switch t.Type {
		case lexer.TokenSlash:
			s += "/"
		case lexer.TokenColon:
			s += ":"
		case lexer.TokenMinus:
			s += "-"
		case lexer.TokenDot:
			s += "."
		case lexer.TokenIdent, lexer.TokenNumber:
			s += t.Lexeme
		case lexer.TokenStar:
			s += "*"
		default:
			return s, nil
		}
		p.advance()
	}
	return s, nil
}

func (p *Parser) parseMiddleware() (Stmt, error) {
	tok := p.advance()
	name, err := p.expect(lexer.TokenIdent, "as middleware name")
	if err != nil {
		return nil, err
	}
	var params []string
	if p.check(lexer.TokenLParen) {
		params, err = p.parseParamList()
		if err != nil {
			return nil, err
		}
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &MiddlewareDecl{pos: mkPos(tok), Name: name.Lexeme, Params: params, Body: body}, nil
}

func (p *Parser) parseUse() (Stmt, error) {
	tok := p.advance()
	name, err := p.expect(lexer.TokenIdent, "as middleware name in `use`")
	if err != nil {
		return nil, err
	}
	p.match(lexer.TokenSemicolon)
	return &UseStmt{pos: mkPos(tok), Name: name.Lexeme}, nil
}

func (p *Parser) parseIf() (Stmt, error) {
	tok := p.advance()
	hadParen := p.match(lexer.TokenLParen)
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if hadParen {
		if _, err := p.expect(lexer.TokenRParen, "to close `if` condition"); err != nil {
			return nil, err
		}
	}
	thenBranch, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	var elseBranch []Stmt
	if p.match(lexer.TokenElse) {
		if p.check(lexer.TokenIf) {
			nested, err := p.parseIf()
			if err != nil {
				return nil, err
			}
			elseBranch = []Stmt{nested}
		} else {
			elseBranch, err = p.parseBlock()
			if err != nil {
				return nil, err
			}
		}
	}
	return &IfStmt{pos: mkPos(tok), Cond: cond, Then: thenBranch, Else: elseBranch}, nil
}

func (p *Parser) parseLoop() (Stmt, error) {
	tok := p.advance()
	iter, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokenAs, "after loop iterable"); err != nil {
		return nil, err
	}
	first, err := p.expect(lexer.TokenIdent, "as loop variable")
	if err != nil {
		return nil, err
	}
	indexVar := ""
	varName := first.Lexeme
	// Optional `, value` form: first is the index, second is the element.
	if p.match(lexer.TokenComma) {
		second, err := p.expect(lexer.TokenIdent, "as second loop variable")
		if err != nil {
			return nil, err
		}
		indexVar = first.Lexeme
		varName = second.Lexeme
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &LoopStmt{pos: mkPos(tok), Iterable: iter, IndexVar: indexVar, Var: varName, Body: body}, nil
}

func (p *Parser) parseWhile() (Stmt, error) {
	tok := p.advance()
	hadParen := p.match(lexer.TokenLParen)
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if hadParen {
		if _, err := p.expect(lexer.TokenRParen, "to close `while` condition"); err != nil {
			return nil, err
		}
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &WhileStmt{pos: mkPos(tok), Cond: cond, Body: body}, nil
}

func (p *Parser) parseTry() (Stmt, error) {
	tok := p.advance()
	tryBody, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokenCatch, "after try block"); err != nil {
		return nil, err
	}
	var catchVar string
	if p.match(lexer.TokenLParen) {
		id, err := p.expect(lexer.TokenIdent, "as catch variable")
		if err != nil {
			return nil, err
		}
		catchVar = id.Lexeme
		if _, err := p.expect(lexer.TokenRParen, "to close catch parameter"); err != nil {
			return nil, err
		}
	} else if p.check(lexer.TokenIdent) {
		catchVar = p.advance().Lexeme
	}
	catchBody, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &TryStmt{pos: mkPos(tok), Try: tryBody, CatchVar: catchVar, Catch: catchBody}, nil
}

func (p *Parser) parseReturn() (Stmt, error) {
	tok := p.advance()
	stmt := &ReturnStmt{pos: mkPos(tok)}
	// A bare `return` is signalled by the next token being a block terminator
	// or another statement-starting keyword that can't begin an expression.
	if !p.check(lexer.TokenRBrace) && !p.check(lexer.TokenSemicolon) && !p.isAtEnd() {
		switch p.cur().Type {
		case lexer.TokenLet, lexer.TokenIf, lexer.TokenLoop, lexer.TokenWhile,
			lexer.TokenRoute, lexer.TokenServer, lexer.TokenMiddleware,
			lexer.TokenUse, lexer.TokenTry, lexer.TokenReturn, lexer.TokenImport,
			lexer.TokenBreak, lexer.TokenContinue:
			// no value — these tokens can't start an expression.
		default:
			val, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			stmt.Value = val
		}
	}
	p.match(lexer.TokenSemicolon)
	return stmt, nil
}

func (p *Parser) parseStatic() (Stmt, error) {
	tok := p.advance()
	dir, err := p.expect(lexer.TokenString, "as static directory path")
	if err != nil {
		return nil, err
	}
	mount := "/"
	// Optional `at "/prefix"` clause.
	if p.check(lexer.TokenIdent) && p.cur().Lexeme == "at" {
		p.advance()
		m, err := p.expect(lexer.TokenString, "as static mount prefix")
		if err != nil {
			return nil, err
		}
		mount = m.Lexeme
	}
	p.match(lexer.TokenSemicolon)
	return &StaticStmt{pos: mkPos(tok), Dir: dir.Lexeme, Mount: mount}, nil
}

func (p *Parser) parseImport() (Stmt, error) {
	tok := p.advance()
	path, err := p.expect(lexer.TokenString, "as import path")
	if err != nil {
		return nil, err
	}
	stmt := &ImportStmt{pos: mkPos(tok), Path: path.Lexeme}
	// Optional `as <ident>` for namespaced imports.
	if p.check(lexer.TokenAs) {
		p.advance()
		alias, err := p.expect(lexer.TokenIdent, "as namespace alias after `as`")
		if err != nil {
			return nil, err
		}
		stmt.As = alias.Lexeme
	}
	p.match(lexer.TokenSemicolon)
	return stmt, nil
}

func (p *Parser) parseExprStmt() (Stmt, error) {
	startTok := p.cur()
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	// Assignment: target = value
	if p.check(lexer.TokenAssign) {
		p.advance()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		p.match(lexer.TokenSemicolon)
		return &AssignStmt{pos: mkPos(startTok), Target: expr, Value: val}, nil
	}
	p.match(lexer.TokenSemicolon)
	return &ExprStmt{pos: mkPos(startTok), Expr: expr}, nil
}

// ===== Expressions (Pratt-ish precedence climbing) =====

func (p *Parser) parseExpr() (Expr, error) { return p.parseNullCoalesce() }

func (p *Parser) parseNullCoalesce() (Expr, error) {
	left, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	for p.check(lexer.TokenNullCoalesce) {
		tok := p.advance()
		right, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{pos: mkPos(tok), Op: "??", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.check(lexer.TokenOr) {
		tok := p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{pos: mkPos(tok), Op: "||", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAnd() (Expr, error) {
	left, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	for p.check(lexer.TokenAnd) {
		tok := p.advance()
		right, err := p.parseEquality()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{pos: mkPos(tok), Op: "&&", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseEquality() (Expr, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.check(lexer.TokenEq) || p.check(lexer.TokenNotEq) {
		tok := p.advance()
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{pos: mkPos(tok), Op: tok.Lexeme, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseComparison() (Expr, error) {
	left, err := p.parseAddition()
	if err != nil {
		return nil, err
	}
	for p.check(lexer.TokenLT) || p.check(lexer.TokenGT) || p.check(lexer.TokenLTEq) || p.check(lexer.TokenGTEq) {
		tok := p.advance()
		right, err := p.parseAddition()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{pos: mkPos(tok), Op: tok.Lexeme, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAddition() (Expr, error) {
	left, err := p.parseMultiplication()
	if err != nil {
		return nil, err
	}
	for p.check(lexer.TokenPlus) || p.check(lexer.TokenMinus) {
		tok := p.advance()
		right, err := p.parseMultiplication()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{pos: mkPos(tok), Op: tok.Lexeme, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseMultiplication() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.check(lexer.TokenStar) || p.check(lexer.TokenSlash) || p.check(lexer.TokenPercent) {
		tok := p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{pos: mkPos(tok), Op: tok.Lexeme, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseUnary() (Expr, error) {
	if p.check(lexer.TokenBang) || p.check(lexer.TokenMinus) {
		tok := p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{pos: mkPos(tok), Op: tok.Lexeme, Operand: operand}, nil
	}
	return p.parseCall()
}

func (p *Parser) parseCall() (Expr, error) {
	expr, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.cur().Type {
		case lexer.TokenLParen:
			tok := p.advance()
			var args []Expr
			if !p.check(lexer.TokenRParen) {
				for {
					var a Expr
					var err error
					if p.check(lexer.TokenSpread) {
						spreadTok := p.advance()
						inner, err := p.parseExpr()
						if err != nil {
							return nil, err
						}
						a = &SpreadExpr{pos: mkPos(spreadTok), Inner: inner}
					} else {
						a, err = p.parseExpr()
						if err != nil {
							return nil, err
						}
					}
					args = append(args, a)
					if !p.match(lexer.TokenComma) {
						break
					}
				}
			}
			if _, err := p.expect(lexer.TokenRParen, "to close call args"); err != nil {
				return nil, err
			}
			expr = &CallExpr{pos: mkPos(tok), Callee: expr, Args: args}
		case lexer.TokenLBracket:
			tok := p.advance()
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.TokenRBracket, "to close index"); err != nil {
				return nil, err
			}
			expr = &IndexExpr{pos: mkPos(tok), Object: expr, Index: idx}
		case lexer.TokenDot:
			tok := p.advance()
			name, err := p.expect(lexer.TokenIdent, "after `.`")
			if err != nil {
				return nil, err
			}
			expr = &MemberExpr{pos: mkPos(tok), Object: expr, Property: name.Lexeme}
		case lexer.TokenQuestionDot:
			tok := p.advance()
			name, err := p.expect(lexer.TokenIdent, "after `?.`")
			if err != nil {
				return nil, err
			}
			expr = &MemberExpr{pos: mkPos(tok), Object: expr, Property: name.Lexeme, Optional: true}
		default:
			return expr, nil
		}
	}
}

func (p *Parser) parsePrimary() (Expr, error) {
	t := p.cur()
	switch t.Type {
	case lexer.TokenNumber:
		p.advance()
		v, err := strconv.ParseFloat(t.Lexeme, 64)
		if err != nil {
			return nil, p.errorf("invalid number %q", t.Lexeme)
		}
		return &NumberLit{pos: mkPos(t), Value: v}, nil
	case lexer.TokenString:
		p.advance()
		return &StringLit{pos: mkPos(t), Value: t.Lexeme}, nil
	case lexer.TokenTrue:
		p.advance()
		return &BoolLit{pos: mkPos(t), Value: true}, nil
	case lexer.TokenFalse:
		p.advance()
		return &BoolLit{pos: mkPos(t), Value: false}, nil
	case lexer.TokenNull:
		p.advance()
		return &NullLit{pos: mkPos(t)}, nil
	case lexer.TokenIdent:
		p.advance()
		return &Identifier{pos: mkPos(t), Name: t.Lexeme}, nil
	case lexer.TokenLParen:
		p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.TokenRParen, "to close grouping"); err != nil {
			return nil, err
		}
		return e, nil
	case lexer.TokenLBracket:
		return p.parseArrayLit()
	case lexer.TokenLBrace:
		return p.parseObjectLit()
	case lexer.TokenFn:
		return p.parseFnLit()
	case lexer.TokenMatch:
		return p.parseMatch()
	case lexer.TokenTry:
		return p.parseTryExpr()
	}
	return nil, p.errorf("unexpected token %s (%q) in expression", t.Type, t.Lexeme)
}

// parseTryExpr is the expression form of try/catch. Mirrors parseTry but
// emits a TryExpr instead of a TryStmt.
func (p *Parser) parseTryExpr() (Expr, error) {
	stmt, err := p.parseTry()
	if err != nil {
		return nil, err
	}
	t := stmt.(*TryStmt)
	return &TryExpr{pos: t.pos, Try: t.Try, CatchVar: t.CatchVar, Catch: t.Catch}, nil
}

// parseMatch parses `match <expr> { pat => expr, pat => expr, _ => expr }`.
func (p *Parser) parseMatch() (Expr, error) {
	tok := p.advance()          // consume `match`
	subject, err := p.parseOr() // parse below ?? to keep `match` standalone-friendly
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokenLBrace, "to start match arms"); err != nil {
		return nil, err
	}
	var arms []MatchArm
	for !p.check(lexer.TokenRBrace) && !p.isAtEnd() {
		var pattern Expr
		if p.check(lexer.TokenIdent) && p.cur().Lexeme == "_" {
			p.advance()
			pattern = nil
		} else {
			pattern, err = p.parseOr()
			if err != nil {
				return nil, err
			}
		}
		if _, err := p.expect(lexer.TokenFatArrow, "between match pattern and body"); err != nil {
			return nil, err
		}
		body, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		arms = append(arms, MatchArm{Pattern: pattern, Body: body})
		if !p.match(lexer.TokenComma) {
			// allow newline-separated arms — the lexer skips newlines as whitespace,
			// so this is just "no comma needed"
		}
		if p.check(lexer.TokenRBrace) {
			break
		}
	}
	if _, err := p.expect(lexer.TokenRBrace, "to close match"); err != nil {
		return nil, err
	}
	return &MatchExpr{pos: mkPos(tok), Subject: subject, Arms: arms}, nil
}

// parseFnLit parses an anonymous function literal: fn(params) { ... }.
func (p *Parser) parseFnLit() (Expr, error) {
	tok := p.advance() // consume fn
	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &FnLit{pos: mkPos(tok), Params: params, Body: body}, nil
}

func (p *Parser) parseArrayLit() (Expr, error) {
	tok := p.advance() // [
	var elems []Expr
	if !p.check(lexer.TokenRBracket) {
		for {
			var e Expr
			var err error
			if p.check(lexer.TokenSpread) {
				spreadTok := p.advance()
				inner, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				e = &SpreadExpr{pos: mkPos(spreadTok), Inner: inner}
			} else {
				e, err = p.parseExpr()
				if err != nil {
					return nil, err
				}
			}
			elems = append(elems, e)
			if !p.match(lexer.TokenComma) {
				break
			}
			if p.check(lexer.TokenRBracket) {
				break
			}
		}
	}
	if _, err := p.expect(lexer.TokenRBracket, "to close array"); err != nil {
		return nil, err
	}
	return &ArrayLit{pos: mkPos(tok), Elements: elems}, nil
}

func (p *Parser) parseObjectLit() (Expr, error) {
	tok := p.advance() // {
	var pairs []ObjectPair
	if !p.check(lexer.TokenRBrace) {
		for {
			// Spread: { ...source }. Encoded as a pair with empty Key so
			// the interpreter knows to expand Value as an object.
			if p.check(lexer.TokenSpread) {
				p.advance()
				v, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				pairs = append(pairs, ObjectPair{Key: "", Value: v})
				if !p.match(lexer.TokenComma) {
					break
				}
				if p.check(lexer.TokenRBrace) {
					break
				}
				continue
			}

			var key string
			switch p.cur().Type {
			case lexer.TokenIdent:
				key = p.advance().Lexeme
			case lexer.TokenString:
				key = p.advance().Lexeme
			default:
				return nil, p.errorf("expected object key, got %s", p.cur().Type)
			}
			if _, err := p.expect(lexer.TokenColon, "after object key"); err != nil {
				return nil, err
			}
			v, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, ObjectPair{Key: key, Value: v})
			if !p.match(lexer.TokenComma) {
				break
			}
			if p.check(lexer.TokenRBrace) {
				break
			}
		}
	}
	if _, err := p.expect(lexer.TokenRBrace, "to close object literal"); err != nil {
		return nil, err
	}
	return &ObjectLit{pos: mkPos(tok), Pairs: pairs}, nil
}
