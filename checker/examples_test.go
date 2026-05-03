package checker

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jlkdevelop/mxscript/lexer"
	"github.com/jlkdevelop/mxscript/parser"
)

// TestEveryBundledExamplePassesChecker walks ../examples and runs
// the static analyzer over each .mx file. Catches drift between the
// language and its showcase code: if a builtin is renamed or removed,
// or a new keyword is introduced, the example breaks here before
// users hit it.
//
// Errors fail the test; warnings (like unused-let) are tolerated.
func TestEveryBundledExamplePassesChecker(t *testing.T) {
	matches, err := filepath.Glob("../examples/*.mx")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no examples found — did the directory move?")
	}
	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src := mustReadFile(t, path)
			tokens, err := lexer.New(src).Tokenize()
			if err != nil {
				t.Fatalf("lex: %v", err)
			}
			prog, err := parser.New(tokens).Parse()
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			diags := Check(prog)
			for _, d := range diags {
				if d.Severity == SeverityError {
					t.Errorf("%s:%d:%d: %s", filepath.Base(path), d.Line, d.Col, d.Message)
				}
			}
		})
	}
}

// mustReadFile is a tiny helper so the test file's import surface
// stays minimal.
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := readFileForTest(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.HasSuffix(path, ".mx") {
		t.Fatalf("not a .mx file: %s", path)
	}
	return string(raw)
}
