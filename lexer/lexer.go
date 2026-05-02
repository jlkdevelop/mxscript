// Package lexer tokenizes MX Script source code into a stream of tokens
// that the parser can consume. It tracks line and column numbers so error
// messages can point at the exact location of a problem.
package lexer

import (
	"fmt"
	"strings"
	"unicode"
)

type TokenType int

const (
	TokenEOF TokenType = iota
	TokenIllegal

	TokenIdent
	TokenNumber
	TokenString

	TokenLet
	TokenFn
	TokenReturn
	TokenIf
	TokenElse
	TokenLoop
	TokenAs
	TokenRoute
	TokenServer
	TokenMiddleware
	TokenUse
	TokenTry
	TokenCatch
	TokenTrue
	TokenFalse
	TokenNull
	TokenImport
	TokenExport
	TokenWhile
	TokenBreak
	TokenContinue
	TokenStatic
	TokenMatch
	TokenFatArrow
	TokenSpawn

	TokenLBrace
	TokenRBrace
	TokenLParen
	TokenRParen
	TokenLBracket
	TokenRBracket
	TokenColon
	TokenComma
	TokenDot
	TokenSemicolon
	TokenAssign
	TokenEq
	TokenNotEq
	TokenLT
	TokenGT
	TokenLTEq
	TokenGTEq
	TokenPlus
	TokenMinus
	TokenStar
	TokenSlash
	TokenPercent
	TokenBang
	TokenAnd
	TokenOr
	TokenSpread
	TokenQuestionDot  // ?.
	TokenNullCoalesce // ??
	TokenComment      // // line, # line, or /* block */ — only emitted when CollectComments is true
	TokenNewline      // line break — only emitted when CollectComments is true
)

var tokenNames = map[TokenType]string{
	TokenEOF:          "EOF",
	TokenIllegal:      "ILLEGAL",
	TokenIdent:        "IDENT",
	TokenNumber:       "NUMBER",
	TokenString:       "STRING",
	TokenLet:          "let",
	TokenFn:           "fn",
	TokenReturn:       "return",
	TokenIf:           "if",
	TokenElse:         "else",
	TokenLoop:         "loop",
	TokenAs:           "as",
	TokenRoute:        "route",
	TokenServer:       "server",
	TokenMiddleware:   "middleware",
	TokenUse:          "use",
	TokenTry:          "try",
	TokenCatch:        "catch",
	TokenTrue:         "true",
	TokenFalse:        "false",
	TokenNull:         "null",
	TokenImport:       "import",
	TokenExport:       "export",
	TokenWhile:        "while",
	TokenBreak:        "break",
	TokenContinue:     "continue",
	TokenStatic:       "static",
	TokenMatch:        "match",
	TokenFatArrow:     "=>",
	TokenSpawn:        "spawn",
	TokenLBrace:       "{",
	TokenRBrace:       "}",
	TokenLParen:       "(",
	TokenRParen:       ")",
	TokenLBracket:     "[",
	TokenRBracket:     "]",
	TokenColon:        ":",
	TokenComma:        ",",
	TokenDot:          ".",
	TokenSemicolon:    ";",
	TokenAssign:       "=",
	TokenEq:           "==",
	TokenNotEq:        "!=",
	TokenLT:           "<",
	TokenGT:           ">",
	TokenLTEq:         "<=",
	TokenGTEq:         ">=",
	TokenPlus:         "+",
	TokenMinus:        "-",
	TokenStar:         "*",
	TokenSlash:        "/",
	TokenPercent:      "%",
	TokenBang:         "!",
	TokenAnd:          "&&",
	TokenOr:           "||",
	TokenSpread:       "...",
	TokenQuestionDot:  "?.",
	TokenNullCoalesce: "??",
	TokenComment:      "COMMENT",
	TokenNewline:      "NEWLINE",
}

func (t TokenType) String() string {
	if s, ok := tokenNames[t]; ok {
		return s
	}
	return fmt.Sprintf("Token(%d)", int(t))
}

var keywords = map[string]TokenType{
	"let":        TokenLet,
	"fn":         TokenFn,
	"return":     TokenReturn,
	"if":         TokenIf,
	"else":       TokenElse,
	"loop":       TokenLoop,
	"as":         TokenAs,
	"route":      TokenRoute,
	"server":     TokenServer,
	"middleware": TokenMiddleware,
	"use":        TokenUse,
	"try":        TokenTry,
	"catch":      TokenCatch,
	"true":       TokenTrue,
	"false":      TokenFalse,
	"null":       TokenNull,
	"import":     TokenImport,
	"export":     TokenExport,
	"while":      TokenWhile,
	"break":      TokenBreak,
	"continue":   TokenContinue,
	"static":     TokenStatic,
	"match":      TokenMatch,
	"spawn":      TokenSpawn,
}

type Token struct {
	Type   TokenType
	Lexeme string
	Line   int
	Col    int
}

func (t Token) String() string {
	if t.Lexeme != "" && t.Type != TokenEOF {
		return fmt.Sprintf("%s(%q) @ %d:%d", t.Type, t.Lexeme, t.Line, t.Col)
	}
	return fmt.Sprintf("%s @ %d:%d", t.Type, t.Line, t.Col)
}

type Lexer struct {
	src    []rune
	pos    int
	line   int
	col    int
	tokens []Token

	// CollectComments controls whether // / # / /* ... */ are kept as
	// TokenComment in the output, and whether \n is emitted as TokenNewline.
	// Defaults to false (the parser doesn't want them); the formatter sets
	// it to true so it can preserve comments and blank lines.
	CollectComments bool
}

func New(src string) *Lexer {
	return &Lexer{src: []rune(src), line: 1, col: 1}
}

// NewWithComments returns a lexer that retains comment tokens for the
// formatter (and any future tooling that wants source fidelity).
func NewWithComments(src string) *Lexer {
	l := New(src)
	l.CollectComments = true
	return l
}

// Tokenize runs the lexer over the source and returns the full token stream.
// It always appends a trailing TokenEOF.
func (l *Lexer) Tokenize() ([]Token, error) {
	for l.pos < len(l.src) {
		if err := l.next(); err != nil {
			return nil, err
		}
	}
	l.tokens = append(l.tokens, Token{Type: TokenEOF, Line: l.line, Col: l.col})
	return l.tokens, nil
}

func (l *Lexer) next() error {
	l.skipWhitespaceAndComments()
	if l.pos >= len(l.src) {
		return nil
	}

	startLine, startCol := l.line, l.col
	c := l.src[l.pos]

	switch {
	case unicode.IsLetter(c) || c == '_':
		l.readIdentifier(startLine, startCol)
	case unicode.IsDigit(c):
		l.readNumber(startLine, startCol)
	case c == '"' || c == '\'':
		return l.readString(c, startLine, startCol)
	default:
		return l.readSymbol(startLine, startCol)
	}
	return nil
}

func (l *Lexer) skipWhitespaceAndComments() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			l.advance()
		case c == '\n':
			line, col := l.line, l.col
			l.advance()
			if l.CollectComments {
				l.tokens = append(l.tokens, Token{Type: TokenNewline, Lexeme: "\n", Line: line, Col: col})
			}
		case c == '/' && l.peek(1) == '/':
			startLine, startCol := l.line, l.col
			start := l.pos
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.advance()
			}
			if l.CollectComments {
				l.tokens = append(l.tokens, Token{Type: TokenComment, Lexeme: string(l.src[start:l.pos]), Line: startLine, Col: startCol})
			}
		case c == '/' && l.peek(1) == '*':
			startLine, startCol := l.line, l.col
			start := l.pos
			l.advance()
			l.advance()
			for l.pos < len(l.src) && !(l.src[l.pos] == '*' && l.peek(1) == '/') {
				l.advance()
			}
			if l.pos < len(l.src) {
				l.advance()
				l.advance()
			}
			if l.CollectComments {
				l.tokens = append(l.tokens, Token{Type: TokenComment, Lexeme: string(l.src[start:l.pos]), Line: startLine, Col: startCol})
			}
		case c == '#':
			startLine, startCol := l.line, l.col
			start := l.pos
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.advance()
			}
			if l.CollectComments {
				l.tokens = append(l.tokens, Token{Type: TokenComment, Lexeme: string(l.src[start:l.pos]), Line: startLine, Col: startCol})
			}
		default:
			return
		}
	}
}

func (l *Lexer) peek(offset int) rune {
	p := l.pos + offset
	if p >= len(l.src) {
		return 0
	}
	return l.src[p]
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	c := l.src[l.pos]
	l.pos++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

func (l *Lexer) readIdentifier(line, col int) {
	start := l.pos
	for l.pos < len(l.src) && (unicode.IsLetter(l.src[l.pos]) || unicode.IsDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
		l.advance()
	}
	lexeme := string(l.src[start:l.pos])
	tt := TokenIdent
	if k, ok := keywords[lexeme]; ok {
		tt = k
	}
	l.tokens = append(l.tokens, Token{Type: tt, Lexeme: lexeme, Line: line, Col: col})
}

// readNumber accepts decimals, floats, and the prefixed forms 0xFF / 0b1010 /
// 0o755. Underscores between digits (1_000_000) are stripped before storage,
// so the parser sees a clean numeric literal.
func (l *Lexer) readNumber(line, col int) {
	start := l.pos

	// Prefixed integer literals: hex, octal, binary.
	if l.src[l.pos] == '0' && l.pos+1 < len(l.src) {
		switch l.src[l.pos+1] {
		case 'x', 'X':
			l.advance()
			l.advance()
			for l.pos < len(l.src) && (isHexDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
				l.advance()
			}
			l.emitNumber(start, line, col, 16)
			return
		case 'b', 'B':
			l.advance()
			l.advance()
			for l.pos < len(l.src) && (l.src[l.pos] == '0' || l.src[l.pos] == '1' || l.src[l.pos] == '_') {
				l.advance()
			}
			l.emitNumber(start, line, col, 2)
			return
		case 'o', 'O':
			l.advance()
			l.advance()
			for l.pos < len(l.src) && ((l.src[l.pos] >= '0' && l.src[l.pos] <= '7') || l.src[l.pos] == '_') {
				l.advance()
			}
			l.emitNumber(start, line, col, 8)
			return
		}
	}

	for l.pos < len(l.src) && (unicode.IsDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
		l.advance()
	}
	if l.pos < len(l.src) && l.src[l.pos] == '.' && l.pos+1 < len(l.src) && unicode.IsDigit(l.src[l.pos+1]) {
		l.advance()
		for l.pos < len(l.src) && (unicode.IsDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
			l.advance()
		}
	}
	l.emitNumber(start, line, col, 10)
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// emitNumber strips underscore separators and converts non-decimal bases
// to a decimal string the parser's strconv.ParseFloat can handle.
func (l *Lexer) emitNumber(start, line, col int, base int) {
	raw := string(l.src[start:l.pos])
	cleaned := strings.ReplaceAll(raw, "_", "")
	lexeme := cleaned

	if base != 10 {
		// Strip the 0x / 0b / 0o prefix and reparse as that base, then emit
		// the value as a decimal string so the parser is none the wiser.
		digits := cleaned[2:]
		if digits == "" {
			lexeme = "0"
		} else {
			n := int64(0)
			for _, c := range digits {
				d := int64(0)
				switch {
				case c >= '0' && c <= '9':
					d = int64(c - '0')
				case c >= 'a' && c <= 'f':
					d = int64(c-'a') + 10
				case c >= 'A' && c <= 'F':
					d = int64(c-'A') + 10
				}
				n = n*int64(base) + d
			}
			lexeme = fmt.Sprintf("%d", n)
		}
	}

	l.tokens = append(l.tokens, Token{Type: TokenNumber, Lexeme: lexeme, Line: line, Col: col})
}

// readString handles plain strings and template strings with `${expr}`
// interpolation. A template string is emitted as a parenthesised chain of
// concatenations: `"x ${y} z"` becomes `( "x " + ( y ) + " z" )`. This lets
// the existing parser handle interpolation with no special-case logic.
func (l *Lexer) readString(quote rune, line, col int) error {
	l.advance() // consume opening quote

	type segment struct {
		isExpr            bool
		literal           string
		exprSrc           string
		exprLine, exprCol int
	}
	var segs []segment
	var lit strings.Builder

	flushLit := func() {
		segs = append(segs, segment{literal: lit.String()})
		lit.Reset()
	}

	for l.pos < len(l.src) && l.src[l.pos] != quote {
		c := l.src[l.pos]

		if c == '\\' && l.pos+1 < len(l.src) {
			next := l.src[l.pos+1]
			switch next {
			case 'n':
				lit.WriteRune('\n')
			case 't':
				lit.WriteRune('\t')
			case 'r':
				lit.WriteRune('\r')
			case '\\':
				lit.WriteRune('\\')
			case '"':
				lit.WriteRune('"')
			case '\'':
				lit.WriteRune('\'')
			case '$':
				lit.WriteRune('$')
			default:
				lit.WriteRune(next)
			}
			l.advance()
			l.advance()
			continue
		}

		if c == '$' && l.peek(1) == '{' {
			flushLit()
			l.advance() // $
			l.advance() // {
			startLine, startCol := l.line, l.col
			var src strings.Builder
			depth := 1
			for l.pos < len(l.src) && depth > 0 {
				ch := l.src[l.pos]
				if ch == '{' {
					depth++
				} else if ch == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
				src.WriteRune(ch)
				l.advance()
			}
			if l.pos >= len(l.src) {
				return fmt.Errorf("unterminated string interpolation at line %d", line)
			}
			l.advance() // consume }
			segs = append(segs, segment{
				isExpr:   true,
				exprSrc:  src.String(),
				exprLine: startLine,
				exprCol:  startCol,
			})
			continue
		}

		lit.WriteRune(c)
		l.advance()
	}

	if l.pos >= len(l.src) {
		return fmt.Errorf("unterminated string at line %d", line)
	}
	l.advance() // consume closing quote
	flushLit()

	// Plain string fast-path.
	if len(segs) == 1 && !segs[0].isExpr {
		l.tokens = append(l.tokens, Token{Type: TokenString, Lexeme: segs[0].literal, Line: line, Col: col})
		return nil
	}

	// Emit (s1 + (e1) + s2 + (e2) + ... + sn) as a token chain.
	l.tokens = append(l.tokens, Token{Type: TokenLParen, Lexeme: "(", Line: line, Col: col})
	for idx, s := range segs {
		if idx > 0 {
			l.tokens = append(l.tokens, Token{Type: TokenPlus, Lexeme: "+", Line: line, Col: col})
		}
		if s.isExpr {
			sub := &Lexer{src: []rune(s.exprSrc), line: s.exprLine, col: s.exprCol}
			subTokens, err := sub.Tokenize()
			if err != nil {
				return err
			}
			l.tokens = append(l.tokens, Token{Type: TokenLParen, Lexeme: "(", Line: s.exprLine, Col: s.exprCol})
			for _, t := range subTokens {
				if t.Type != TokenEOF {
					l.tokens = append(l.tokens, t)
				}
			}
			l.tokens = append(l.tokens, Token{Type: TokenRParen, Lexeme: ")", Line: s.exprLine, Col: s.exprCol})
		} else {
			l.tokens = append(l.tokens, Token{Type: TokenString, Lexeme: s.literal, Line: line, Col: col})
		}
	}
	l.tokens = append(l.tokens, Token{Type: TokenRParen, Lexeme: ")", Line: line, Col: col})
	return nil
}

func (l *Lexer) readSymbol(line, col int) error {
	c := l.src[l.pos]
	// Three-dot spread operator.
	if c == '.' && l.peek(1) == '.' && l.peek(2) == '.' {
		l.advance()
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenSpread, Lexeme: "...", Line: line, Col: col})
		return nil
	}
	two := ""
	if l.pos+1 < len(l.src) {
		two = string(c) + string(l.src[l.pos+1])
	}

	switch two {
	case "?.":
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenQuestionDot, Lexeme: "?.", Line: line, Col: col})
		return nil
	case "??":
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenNullCoalesce, Lexeme: "??", Line: line, Col: col})
		return nil
	case "==":
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenEq, Lexeme: "==", Line: line, Col: col})
		return nil
	case "=>":
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenFatArrow, Lexeme: "=>", Line: line, Col: col})
		return nil
	case "!=":
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenNotEq, Lexeme: "!=", Line: line, Col: col})
		return nil
	case "<=":
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenLTEq, Lexeme: "<=", Line: line, Col: col})
		return nil
	case ">=":
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenGTEq, Lexeme: ">=", Line: line, Col: col})
		return nil
	case "&&":
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenAnd, Lexeme: "&&", Line: line, Col: col})
		return nil
	case "||":
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenOr, Lexeme: "||", Line: line, Col: col})
		return nil
	}

	var tt TokenType
	switch c {
	case '{':
		tt = TokenLBrace
	case '}':
		tt = TokenRBrace
	case '(':
		tt = TokenLParen
	case ')':
		tt = TokenRParen
	case '[':
		tt = TokenLBracket
	case ']':
		tt = TokenRBracket
	case ':':
		tt = TokenColon
	case ',':
		tt = TokenComma
	case '.':
		tt = TokenDot
	case ';':
		tt = TokenSemicolon
	case '=':
		tt = TokenAssign
	case '<':
		tt = TokenLT
	case '>':
		tt = TokenGT
	case '+':
		tt = TokenPlus
	case '-':
		tt = TokenMinus
	case '*':
		tt = TokenStar
	case '/':
		tt = TokenSlash
	case '%':
		tt = TokenPercent
	case '!':
		tt = TokenBang
	default:
		return fmt.Errorf("unexpected character %q at line %d col %d", c, line, col)
	}
	l.advance()
	l.tokens = append(l.tokens, Token{Type: tt, Lexeme: string(c), Line: line, Col: col})
	return nil
}
