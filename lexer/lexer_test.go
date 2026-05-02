// lexer_test.go covers token boundaries, keyword recognition, and string escapes.
package lexer

import "testing"

func tokenize(t *testing.T, src string) []Token {
	t.Helper()
	tokens, err := New(src).Tokenize()
	if err != nil {
		t.Fatalf("tokenize error: %v", err)
	}
	return tokens
}

func types(tokens []Token) []TokenType {
	out := make([]TokenType, 0, len(tokens))
	for _, tk := range tokens {
		if tk.Type == TokenEOF {
			continue
		}
		out = append(out, tk.Type)
	}
	return out
}

func TestKeywordsAndIdentifiers(t *testing.T) {
	got := types(tokenize(t, "let x = 10 fn greet(name) { return name }"))
	want := []TokenType{
		TokenLet, TokenIdent, TokenAssign, TokenNumber,
		TokenFn, TokenIdent, TokenLParen, TokenIdent, TokenRParen,
		TokenLBrace, TokenReturn, TokenIdent, TokenRBrace,
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %s, want %s", i, got[i], want[i])
		}
	}
}

func TestStringEscapes(t *testing.T) {
	tokens := tokenize(t, `"hello\nworld\t\"quoted\""`)
	if tokens[0].Type != TokenString {
		t.Fatalf("expected string token, got %s", tokens[0].Type)
	}
	want := "hello\nworld\t\"quoted\""
	if tokens[0].Lexeme != want {
		t.Errorf("got %q, want %q", tokens[0].Lexeme, want)
	}
}

func TestTwoCharOperators(t *testing.T) {
	got := types(tokenize(t, "a == b != c <= d >= e && f || g"))
	want := []TokenType{
		TokenIdent, TokenEq, TokenIdent,
		TokenNotEq, TokenIdent,
		TokenLTEq, TokenIdent,
		TokenGTEq, TokenIdent,
		TokenAnd, TokenIdent,
		TokenOr, TokenIdent,
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %s, want %s", i, got[i], want[i])
		}
	}
}

func TestLineColTracking(t *testing.T) {
	tokens := tokenize(t, "let x = 1\nlet y = 2")
	// First `let` is line 1
	if tokens[0].Line != 1 {
		t.Errorf("first let: got line %d, want 1", tokens[0].Line)
	}
	// Second `let` is line 2 — find it
	var second Token
	count := 0
	for _, tk := range tokens {
		if tk.Type == TokenLet {
			count++
			if count == 2 {
				second = tk
			}
		}
	}
	if second.Line != 2 {
		t.Errorf("second let: got line %d, want 2", second.Line)
	}
}

func TestComments(t *testing.T) {
	src := `// line comment
let x = 1 // trailing
/* block comment */
# hash comment
let y = 2`
	got := types(tokenize(t, src))
	want := []TokenType{
		TokenLet, TokenIdent, TokenAssign, TokenNumber,
		TokenLet, TokenIdent, TokenAssign, TokenNumber,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tokens, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %s, want %s", i, got[i], want[i])
		}
	}
}
