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
)

var tokenNames = map[TokenType]string{
	TokenEOF:        "EOF",
	TokenIllegal:    "ILLEGAL",
	TokenIdent:      "IDENT",
	TokenNumber:     "NUMBER",
	TokenString:     "STRING",
	TokenLet:        "let",
	TokenFn:         "fn",
	TokenReturn:     "return",
	TokenIf:         "if",
	TokenElse:       "else",
	TokenLoop:       "loop",
	TokenAs:         "as",
	TokenRoute:      "route",
	TokenServer:     "server",
	TokenMiddleware: "middleware",
	TokenUse:        "use",
	TokenTry:        "try",
	TokenCatch:      "catch",
	TokenTrue:       "true",
	TokenFalse:      "false",
	TokenNull:       "null",
	TokenImport:     "import",
	TokenExport:     "export",
	TokenLBrace:     "{",
	TokenRBrace:     "}",
	TokenLParen:     "(",
	TokenRParen:     ")",
	TokenLBracket:   "[",
	TokenRBracket:   "]",
	TokenColon:      ":",
	TokenComma:      ",",
	TokenDot:        ".",
	TokenSemicolon:  ";",
	TokenAssign:     "=",
	TokenEq:         "==",
	TokenNotEq:      "!=",
	TokenLT:         "<",
	TokenGT:         ">",
	TokenLTEq:       "<=",
	TokenGTEq:       ">=",
	TokenPlus:       "+",
	TokenMinus:      "-",
	TokenStar:       "*",
	TokenSlash:      "/",
	TokenPercent:    "%",
	TokenBang:       "!",
	TokenAnd:        "&&",
	TokenOr:         "||",
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
}

func New(src string) *Lexer {
	return &Lexer{src: []rune(src), line: 1, col: 1}
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
			l.advance()
		case c == '/' && l.peek(1) == '/':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.advance()
			}
		case c == '/' && l.peek(1) == '*':
			l.advance()
			l.advance()
			for l.pos < len(l.src) && !(l.src[l.pos] == '*' && l.peek(1) == '/') {
				l.advance()
			}
			if l.pos < len(l.src) {
				l.advance()
				l.advance()
			}
		case c == '#':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.advance()
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

func (l *Lexer) readNumber(line, col int) {
	start := l.pos
	for l.pos < len(l.src) && unicode.IsDigit(l.src[l.pos]) {
		l.advance()
	}
	if l.pos < len(l.src) && l.src[l.pos] == '.' && l.pos+1 < len(l.src) && unicode.IsDigit(l.src[l.pos+1]) {
		l.advance()
		for l.pos < len(l.src) && unicode.IsDigit(l.src[l.pos]) {
			l.advance()
		}
	}
	l.tokens = append(l.tokens, Token{Type: TokenNumber, Lexeme: string(l.src[start:l.pos]), Line: line, Col: col})
}

func (l *Lexer) readString(quote rune, line, col int) error {
	l.advance()
	var b strings.Builder
	for l.pos < len(l.src) && l.src[l.pos] != quote {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) {
			next := l.src[l.pos+1]
			switch next {
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			case 'r':
				b.WriteRune('\r')
			case '\\':
				b.WriteRune('\\')
			case '"':
				b.WriteRune('"')
			case '\'':
				b.WriteRune('\'')
			default:
				b.WriteRune(next)
			}
			l.advance()
			l.advance()
			continue
		}
		b.WriteRune(c)
		l.advance()
	}
	if l.pos >= len(l.src) {
		return fmt.Errorf("unterminated string at line %d", line)
	}
	l.advance()
	l.tokens = append(l.tokens, Token{Type: TokenString, Lexeme: b.String(), Line: line, Col: col})
	return nil
}

func (l *Lexer) readSymbol(line, col int) error {
	c := l.src[l.pos]
	two := ""
	if l.pos+1 < len(l.src) {
		two = string(c) + string(l.src[l.pos+1])
	}

	switch two {
	case "==":
		l.advance()
		l.advance()
		l.tokens = append(l.tokens, Token{Type: TokenEq, Lexeme: "==", Line: line, Col: col})
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
