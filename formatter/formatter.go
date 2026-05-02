// Package formatter produces a canonical, opinionated formatting of an
// MX Script source file. It walks the token stream (with comments and
// newlines preserved by the lexer's CollectComments flag) and emits a
// new source string with consistent indentation and operator spacing.
//
// Conventions:
//   - 2-space indent, scoped by `{`/`}`/`(`/`)`/`[`/`]` depth.
//   - One blank line max between statements; blank lines inside blocks
//     are preserved (a single newline stays a single newline; two or
//     more collapse to one blank line).
//   - Standard operator spacing:  a + b, a == b, a && b, ...
//   - Comma-separated lists: `a, b, c` (no leading space, trailing
//     space).
//   - Braces: `{` stays on the same line as the keyword, `}` on its
//     own line.
//   - Comments: preserved in place. A trailing `// ...` keeps its
//     trailing position; a leading `// ...` stays above the next
//     statement.
package formatter

import (
	"strings"

	"github.com/jlkdevelop/mxscript/lexer"
)

// Format normalizes the source string. Returns the formatted source and
// any lexing error.
func Format(src string) (string, error) {
	tokens, err := lexer.NewWithComments(src).Tokenize()
	if err != nil {
		return "", err
	}
	f := &formatter{tokens: tokens}
	return f.run(), nil
}

type formatter struct {
	tokens      []lexer.Token
	pos         int
	out         strings.Builder
	indent      int
	col         int // current column on the output line
	atLineStart bool

	// pathMode is true while emitting a route path (between a method
	// keyword and the opening `{`). Path tokens get tight spacing.
	pathMode      bool
	pathFirst     bool // true for the first token in pathMode (needs leading space)
	skipNextBlank bool // suppress one TokenNewline when we just emitted a block-opener newline

	// braceKinds tracks whether each open `{` is a block (newline after)
	// or an expression / object literal (tight). Top of stack is current.
	braceKinds []braceKind
}

type braceKind int

const (
	braceBlock braceKind = iota
	braceExpr
)

const indentStep = "  "

func (f *formatter) run() string {
	f.atLineStart = true
	for f.pos < len(f.tokens) {
		t := f.tokens[f.pos]
		if t.Type == lexer.TokenEOF {
			break
		}
		f.emit(t)
		f.pos++
	}
	out := f.out.String()
	// Collapse trailing whitespace on each line.
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t")
	}
	out = strings.Join(lines, "\n")
	// Collapse 3+ blank lines down to 1.
	for strings.Contains(out, "\n\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n\n", "\n\n\n")
	}
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func (f *formatter) emit(t lexer.Token) {
	// Path mode: inside a route path, glue tokens together with no spaces.
	if f.pathMode {
		switch t.Type {
		case lexer.TokenLBrace:
			f.pathMode = false
			f.pathFirst = false
			// fall through to the LBrace case below
		case lexer.TokenNewline, lexer.TokenComment:
			// allowed but treated normally
		default:
			if f.pathFirst {
				f.writeSpace()
				f.pathFirst = false
			}
			f.writeRaw(t.Lexeme)
			return
		}
	}

	// Suppress one redundant newline right after a block-opener.
	if f.skipNextBlank && t.Type == lexer.TokenNewline {
		f.skipNextBlank = false
		return
	}
	if t.Type != lexer.TokenNewline && t.Type != lexer.TokenComment {
		f.skipNextBlank = false
	}

	switch t.Type {
	case lexer.TokenNewline:
		f.writeNewline()
	case lexer.TokenComment:
		f.writeComment(t.Lexeme)
	case lexer.TokenLBrace:
		kind := f.classifyBraceOpen()
		if !f.atLineStart {
			f.writeSpace()
		} else {
			f.writeIndent()
		}
		f.writeRaw("{")
		f.indent++
		f.braceKinds = append(f.braceKinds, kind)
		if kind == braceBlock {
			// Newline so block contents start on their own line.
			f.writeNewline()
			// Don't double up if the source also has a newline next.
			f.skipNextBlank = true
		}
	case lexer.TokenRBrace:
		f.indent--
		var kind braceKind
		if len(f.braceKinds) > 0 {
			kind = f.braceKinds[len(f.braceKinds)-1]
			f.braceKinds = f.braceKinds[:len(f.braceKinds)-1]
		}
		if kind == braceBlock {
			if !f.atLineStart {
				f.writeNewline()
			}
			f.writeIndent()
			f.writeRaw("}")
		} else {
			// Object literal: keep `}` tight if we're still on the value's line,
			// or indent if a newline already happened.
			if f.atLineStart {
				f.writeIndent()
			}
			f.writeRaw("}")
		}
	case lexer.TokenLParen, lexer.TokenLBracket:
		// Tight on the left for calls / index / array / paren-grouping.
		if f.atLineStart {
			f.writeIndent()
		}
		f.writeRaw(t.Lexeme)
	case lexer.TokenRParen, lexer.TokenRBracket:
		f.writeRaw(t.Lexeme)
	case lexer.TokenComma:
		f.writeRaw(",")
		// Space follows unless next is a closing bracket / RParen.
		if next := f.peekSignificant(); next != nil && next.Type != lexer.TokenRBracket && next.Type != lexer.TokenRParen {
			f.writeSpace()
		}
	case lexer.TokenSemicolon:
		f.writeRaw(";")
	case lexer.TokenColon:
		// Tight on the left, space on the right (object literals, server config).
		f.writeRaw(":")
		f.writeSpace()
	case lexer.TokenDot, lexer.TokenQuestionDot, lexer.TokenSpread:
		// Tight on both sides.
		f.writeRaw(t.Lexeme)
	case lexer.TokenAssign, lexer.TokenEq, lexer.TokenNotEq,
		lexer.TokenLT, lexer.TokenGT, lexer.TokenLTEq, lexer.TokenGTEq,
		lexer.TokenAnd, lexer.TokenOr, lexer.TokenNullCoalesce,
		lexer.TokenFatArrow,
		lexer.TokenPlus, lexer.TokenStar, lexer.TokenPercent:
		// Spaces on both sides.
		if !f.atLineStart {
			f.writeSpace()
		} else {
			f.writeIndent()
		}
		f.writeRaw(t.Lexeme)
		f.writeSpace()
	case lexer.TokenMinus, lexer.TokenSlash:
		// Could be unary or binary. If the previous significant token is
		// an operator/keyword/(/,/[/=/return, treat as unary (tight on right).
		if f.isUnaryContext() {
			if f.atLineStart {
				f.writeIndent()
			}
			f.writeRaw(t.Lexeme)
		} else {
			if !f.atLineStart {
				f.writeSpace()
			} else {
				f.writeIndent()
			}
			f.writeRaw(t.Lexeme)
			f.writeSpace()
		}
	case lexer.TokenBang:
		// Always unary.
		if f.atLineStart {
			f.writeIndent()
		}
		f.writeRaw("!")
	case lexer.TokenString:
		if f.atLineStart {
			f.writeIndent()
		} else if f.needSpaceBeforeWord() {
			f.writeSpace()
		}
		f.writeRaw(quoteString(t.Lexeme))
	default:
		// Identifiers, numbers, keywords.
		if f.atLineStart {
			f.writeIndent()
		} else if f.needSpaceBeforeWord() {
			f.writeSpace()
		}
		f.writeRaw(t.Lexeme)
		// After a path-introducing keyword, switch to pathMode for the
		// next run of `/`/IDENT/`:` tokens.
		if f.startsRoutePath(t) {
			f.pathMode = true
			f.pathFirst = true
		}
	}
}

// startsRoutePath reports whether `t` is a route-introducing keyword
// (so the immediately-following slash should be tight).
//   - TokenRoute followed by an HTTP method then a path: `route` is the
//     keyword we want to flag, but the actual path starts after the
//     method ident. We flip pathMode after the method.
//   - The shorthand verbs (get/post/put/delete/patch/head/options/sse)
//     come in as identifiers — we detect by lexeme + a following `/`.
func (f *formatter) startsRoutePath(t lexer.Token) bool {
	if t.Type == lexer.TokenIdent {
		// Shorthand HTTP method or the method after `route`.
		switch t.Lexeme {
		case "get", "post", "put", "delete", "patch", "head", "options", "sse",
			"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
			// Only treat as path-starting if next significant token is `/`.
			next := f.peekSignificant()
			return next != nil && next.Type == lexer.TokenSlash
		}
	}
	return false
}

// classifyBraceOpen decides whether the upcoming `{` opens a block
// (statement context — body of fn/route/if/loop/...) or an expression
// (object literal). Heuristic: look at the previous significant token.
func (f *formatter) classifyBraceOpen() braceKind {
	prev := f.prevSignificant()
	if prev == nil {
		return braceBlock
	}
	switch prev.Type {
	case lexer.TokenAssign, lexer.TokenColon, lexer.TokenComma,
		lexer.TokenLParen, lexer.TokenLBracket,
		lexer.TokenReturn, lexer.TokenFatArrow,
		lexer.TokenSpread,
		lexer.TokenAnd, lexer.TokenOr, lexer.TokenNullCoalesce,
		lexer.TokenEq, lexer.TokenNotEq:
		return braceExpr
	}
	return braceBlock
}

func (f *formatter) writeRaw(s string) {
	f.out.WriteString(s)
	f.col += len(s)
	f.atLineStart = false
}

func (f *formatter) writeSpace() {
	if f.atLineStart {
		f.writeIndent()
		return
	}
	if f.col > 0 && !endsWithSpace(f.out.String()) {
		f.out.WriteByte(' ')
		f.col++
	}
}

func (f *formatter) writeIndent() {
	for i := 0; i < f.indent; i++ {
		f.out.WriteString(indentStep)
	}
	f.col = f.indent * len(indentStep)
	f.atLineStart = false
}

func (f *formatter) writeNewline() {
	f.out.WriteByte('\n')
	f.col = 0
	f.atLineStart = true
}

func (f *formatter) writeComment(text string) {
	if f.atLineStart {
		f.writeIndent()
	} else {
		f.writeSpace()
	}
	f.writeRaw(text)
}

// peekSignificant returns the next non-newline, non-comment token, or nil.
func (f *formatter) peekSignificant() *lexer.Token {
	for i := f.pos + 1; i < len(f.tokens); i++ {
		t := &f.tokens[i]
		if t.Type == lexer.TokenNewline || t.Type == lexer.TokenComment {
			continue
		}
		if t.Type == lexer.TokenEOF {
			return nil
		}
		return t
	}
	return nil
}

// prevSignificant looks BACKWARDS through already-emitted tokens.
func (f *formatter) prevSignificant() *lexer.Token {
	for i := f.pos - 1; i >= 0; i-- {
		t := &f.tokens[i]
		if t.Type == lexer.TokenNewline || t.Type == lexer.TokenComment {
			continue
		}
		return t
	}
	return nil
}

func (f *formatter) isUnaryContext() bool {
	prev := f.prevSignificant()
	if prev == nil {
		return true
	}
	switch prev.Type {
	case lexer.TokenLParen, lexer.TokenLBracket, lexer.TokenLBrace,
		lexer.TokenComma, lexer.TokenColon, lexer.TokenSemicolon,
		lexer.TokenAssign, lexer.TokenEq, lexer.TokenNotEq,
		lexer.TokenLT, lexer.TokenGT, lexer.TokenLTEq, lexer.TokenGTEq,
		lexer.TokenAnd, lexer.TokenOr, lexer.TokenNullCoalesce,
		lexer.TokenPlus, lexer.TokenMinus, lexer.TokenStar, lexer.TokenSlash, lexer.TokenPercent,
		lexer.TokenBang, lexer.TokenReturn, lexer.TokenFatArrow:
		return true
	}
	return false
}

// needSpaceBeforeWord — when emitting an identifier/number/string mid-line,
// we want a space if the previous non-newline emit wasn't already a space-
// equivalent (open paren / dot / spread / etc.).
func (f *formatter) needSpaceBeforeWord() bool {
	cur := f.out.String()
	if cur == "" {
		return false
	}
	last := cur[len(cur)-1]
	switch last {
	case ' ', '\t', '\n', '(', '[', '{', '.', ':':
		return false
	}
	// After a `-` or `!` that was unary, no space.
	prev := f.prevSignificant()
	if prev != nil && (prev.Type == lexer.TokenBang) {
		return false
	}
	return true
}

func endsWithSpace(s string) bool {
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	return last == ' ' || last == '\t' || last == '\n'
}

// quoteString re-encodes a string literal value with double quotes and
// the standard escapes, since the lexer stores the unescaped runes.
func quoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
