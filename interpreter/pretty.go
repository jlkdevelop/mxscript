// pretty.go — pp(value) pretty-printer. Indented, color-aware,
// cycle-safe (well, cycles aren't possible in the value model since
// arrays + objects copy, but we still cap recursion depth).
//
// The REPL uses prettyValue() automatically when displaying results;
// scripts can call pp() explicitly for debugging output.
package interpreter

import (
	"fmt"
	"os"
	"strings"
)

// PrettyDisplay returns a colored, indented representation of v
// suitable for terminal output. Used by the REPL to display results
// so objects + arrays don't render as one-line JSON. Pass colors=false
// for plain output (e.g. when piping to a file).
func PrettyDisplay(v Value, colors bool) string {
	return prettyValue(v, "", colors, 0)
}

// pp(value, opts?) prints a value in a human-friendly form. Returns
// the value unchanged so it composes with chained expressions:
//
//   let user = pp(get_user(id))   // logs the user, then assigns it
func builtinPP(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return NullValue(), nil
	}
	v := args[0]
	colors := isTerminal(os.Stdout)
	if len(args) > 1 && args[1].Kind == KindObject {
		if c, ok := args[1].Object.Get("colors"); ok && c.Kind == KindBool {
			colors = c.Bool
		}
	}
	fmt.Println(prettyValue(v, "", colors, 0))
	return v, nil
}

// prettyValue renders v indented with `indent` as the base prefix.
// `depth` guards against runaway recursion at 10 levels deep.
func prettyValue(v Value, indent string, colors bool, depth int) string {
	if depth > 10 {
		return colorize("...", colors, "33") // yellow
	}
	switch v.Kind {
	case KindNull:
		return colorize("null", colors, "90") // gray
	case KindBool:
		if v.Bool {
			return colorize("true", colors, "32") // green
		}
		return colorize("false", colors, "31") // red
	case KindNumber:
		return colorize(v.Display(), colors, "33") // yellow
	case KindString:
		return colorize(`"`+escapeForDisplay(v.String)+`"`, colors, "36") // cyan
	case KindArray:
		if len(v.Array) == 0 {
			return "[]"
		}
		// Short arrays: inline. Long: one per line.
		if shortArr(v.Array) {
			parts := make([]string, len(v.Array))
			for i, el := range v.Array {
				parts[i] = prettyValue(el, indent, colors, depth+1)
			}
			return "[" + strings.Join(parts, ", ") + "]"
		}
		var b strings.Builder
		b.WriteString("[\n")
		next := indent + "  "
		for i, el := range v.Array {
			b.WriteString(next)
			b.WriteString(prettyValue(el, next, colors, depth+1))
			if i < len(v.Array)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(indent)
		b.WriteByte(']')
		return b.String()
	case KindObject:
		if len(v.Object.Keys) == 0 {
			return "{}"
		}
		var b strings.Builder
		b.WriteString("{\n")
		next := indent + "  "
		for i, k := range v.Object.Keys {
			val, _ := v.Object.Get(k)
			b.WriteString(next)
			b.WriteString(colorize(k, colors, "35")) // magenta keys
			b.WriteString(": ")
			b.WriteString(prettyValue(val, next, colors, depth+1))
			if i < len(v.Object.Keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(indent)
		b.WriteByte('}')
		return b.String()
	case KindFunction:
		if v.Function != nil && v.Function.Name != "" {
			return colorize("<fn "+v.Function.Name+">", colors, "34") // blue
		}
		return colorize("<fn>", colors, "34")
	case KindChannel:
		return colorize("<channel>", colors, "34")
	case KindHandle:
		return colorize("<handle>", colors, "34")
	case KindResponse:
		return colorize("<response>", colors, "34")
	}
	return v.Display()
}

func shortArr(arr []Value) bool {
	if len(arr) > 6 {
		return false
	}
	total := 0
	for _, el := range arr {
		if el.Kind == KindArray || el.Kind == KindObject {
			return false
		}
		total += len(el.Display())
		if total > 60 {
			return false
		}
	}
	return true
}

func escapeForDisplay(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func colorize(s string, on bool, code string) string {
	if !on {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

// isTerminal reports whether the given file is a terminal. We avoid
// pulling in golang.org/x/term to keep dependencies tight; the simple
// check below covers >99% of real usage.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
