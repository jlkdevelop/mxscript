// parse_helper.go wires the interpreter's import statement to the lexer
// and parser without forcing the consumer of this package to do it themselves.
package interpreter

import (
	"github.com/jlkdevelop/mxscript/lexer"
	"github.com/jlkdevelop/mxscript/parser"
)

// ParseSource lexes and parses a string of MX Script source code.
func ParseSource(src string) (*parser.Program, error) {
	tokens, err := lexer.New(src).Tokenize()
	if err != nil {
		return nil, err
	}
	return parser.New(tokens).Parse()
}
