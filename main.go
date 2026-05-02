// main is the MX Script command-line entry point. It provides:
//
//	mx run app.mx [--port N] [--watch] [--debug]
//	mx init [name]
//	mx build app.mx
//	mx version
//	mx help
//
// The CLI is intentionally tiny — every feature delegates to one of the
// language packages (lexer / parser / interpreter).
package main

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jlkdevelop/mxscript/formatter"
	"github.com/jlkdevelop/mxscript/interpreter"
	"github.com/jlkdevelop/mxscript/lexer"
	"github.com/jlkdevelop/mxscript/lsp"
	"github.com/jlkdevelop/mxscript/parser"
)

// replReader is a simple line-buffered stdin reader for the REPL.
type replReader struct{ r *bufio.Reader }

func newReplReader() *replReader { return &replReader{r: bufio.NewReader(os.Stdin)} }

// ReadLine returns one line of input (without the trailing newline). The
// second return value is false on EOF.
func (rr *replReader) ReadLine() (string, bool) {
	line, err := rr.r.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimRight(line, "\r\n"), true
}

// Version is bumped at release time. Override at build with:
//
//	go build -ldflags "-X main.Version=v0.2.0"
var Version = "v0.40.0"

const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cGreen  = "\033[1;32m"
	cYellow = "\033[1;33m"
	cCyan   = "\033[1;36m"
	cRed    = "\033[1;31m"
	cGray   = "\033[0;90m"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "run":
		cmdRun(args)
	case "init":
		cmdInit(args)
	case "build":
		cmdBuild(args)
	case "repl":
		cmdRepl(args)
	case "test":
		cmdTest(args)
	case "bench":
		cmdBench(args)
	case "fmt":
		cmdFmt(args)
	case "lsp":
		cmdLSP(args)
	case "version", "-v", "--version":
		fmt.Println("MX Script", Version)
	case "help", "-h", "--help":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "%sunknown command:%s %s\n\n", cRed, cReset, cmd)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf("%sMX Script%s %s — a modern open-source language for web apps and APIs\n\n",
		cGreen, cReset, Version)
	fmt.Println("Usage: mx <command> [args]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  run <file.mx>         Run an MX Script file")
	fmt.Println("  init [name]           Scaffold a new MX Script project")
	fmt.Println("  build <file.mx>       Type-check & validate an MX Script file")
	fmt.Println("  build --vercel        Generate a Vercel-deployable Go project from app.mx")
	fmt.Println("  repl                  Start an interactive REPL")
	fmt.Println("  test [path]           Run *_test.mx files (default: current dir)")
	fmt.Println("  bench [path]          Run *_bench.mx benchmarks (each fn bench_*)")
	fmt.Println("  fmt [paths]           Format .mx files (-w writes, --check exits 1 on diffs)")
	fmt.Println("  lsp                   Run the Language Server (JSON-RPC over stdio)")
	fmt.Println("  version               Print version and exit")
	fmt.Println("  help                  Show this help")
	fmt.Println()
	fmt.Println("Flags for `run`:")
	fmt.Println("  --port N              Override server.port (default 8080)")
	fmt.Println("  --watch               Restart on file changes (hot reload)")
	fmt.Println("  --debug               Print tokens, AST, and verbose errors")
	fmt.Println()
	fmt.Printf("%sFounded by Jassim Alkharafi · github.com/jlkdevelop/mxscript%s\n", cGray, cReset)
}

// ===== mx run =====

func cmdRun(args []string) {
	var file string
	var eval string
	port := 0
	watch := false
	debug := false

	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--port":
			if i+1 >= len(args) {
				fatal("--port requires a number")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				fatal("--port must be a number")
			}
			port = n
			i += 2
		case strings.HasPrefix(a, "--port="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--port="))
			if err != nil {
				fatal("--port must be a number")
			}
			port = n
			i++
		case a == "--eval", a == "-e":
			if i+1 >= len(args) {
				fatal("--eval requires a snippet string")
			}
			eval = args[i+1]
			i += 2
		case strings.HasPrefix(a, "--eval="):
			eval = strings.TrimPrefix(a, "--eval=")
			i++
		case a == "--watch":
			watch = true
			i++
		case a == "--debug":
			debug = true
			i++
		default:
			if file == "" {
				file = a
			}
			i++
		}
	}

	if eval != "" {
		if err := runSource("<eval>", []byte(eval), port, debug); err != nil {
			printError("<eval>", err)
			os.Exit(1)
		}
		return
	}

	if file == "" {
		fatal("usage: mx run <file.mx> | mx run --eval '<snippet>'")
	}

	if watch {
		runWatched(file, port, debug)
		return
	}

	if err := runOnce(file, port, debug); err != nil {
		printError(file, err)
		os.Exit(1)
	}
}

// printError formats an MX Script error with source context: the offending
// line is shown in red with a caret pointing at the column. If a runtime
// stack is attached (from interpreter.MXError), it's printed below.
func printError(file string, err error) {
	line, col, msg := errorLocation(err)

	fmt.Fprintf(os.Stderr, "\n%serror:%s %s\n", cRed, cReset, msg)
	if line > 0 {
		fmt.Fprintf(os.Stderr, "  %s-->%s %s:%d:%d\n", cGray, cReset, file, line, col)
		src, readErr := os.ReadFile(file)
		if readErr == nil {
			lines := strings.Split(string(src), "\n")
			if line-1 < len(lines) {
				lineStr := strconv.Itoa(line)
				pad := strings.Repeat(" ", len(lineStr))
				fmt.Fprintf(os.Stderr, "   %s%s |%s\n", cGray, pad, cReset)
				fmt.Fprintf(os.Stderr, "   %s%s |%s %s\n", cYellow, lineStr, cReset, lines[line-1])
				caretPad := strings.Repeat(" ", col-1)
				if col < 1 {
					caretPad = ""
				}
				fmt.Fprintf(os.Stderr, "   %s%s |%s %s%s^%s\n", cGray, pad, cReset, caretPad, cRed, cReset)
			}
		}
	}

	var rt *interpreter.MXError
	if errors.As(err, &rt) && len(rt.Stack) > 0 {
		fmt.Fprintf(os.Stderr, "\n  %sstack:%s\n", cYellow, cReset)
		for k := len(rt.Stack) - 1; k >= 0; k-- {
			f := rt.Stack[k]
			fmt.Fprintf(os.Stderr, "    in %s%s%s", cBold, f.Name, cReset)
			if f.Line > 0 {
				fmt.Fprintf(os.Stderr, " %s(called at %s:%d:%d)%s", cGray, file, f.Line, f.Col, cReset)
			}
			fmt.Fprintln(os.Stderr)
		}
	}
	fmt.Fprintln(os.Stderr)
}

func errorLocation(err error) (line, col int, msg string) {
	var pe *parser.ParseError
	if errors.As(err, &pe) {
		return pe.Line, pe.Col, pe.Message
	}
	var rt *interpreter.MXError
	if errors.As(err, &rt) {
		return rt.Line, rt.Col, rt.Message
	}
	return 0, 0, err.Error()
}

func runOnce(file string, port int, debug bool) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", file, err)
	}
	return runSource(file, src, port, debug)
}

func runSource(file string, src []byte, port int, debug bool) error {
	tokens, err := lexer.New(string(src)).Tokenize()
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	if debug {
		fmt.Fprintln(os.Stderr, cYellow+"--- tokens ---"+cReset)
		for _, t := range tokens {
			fmt.Fprintln(os.Stderr, "  ", t)
		}
	}

	prog, err := parser.New(tokens).Parse()
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	if debug {
		fmt.Fprintln(os.Stderr, cYellow+"--- AST ---"+cReset)
		for _, s := range prog.Stmts {
			fmt.Fprintf(os.Stderr, "  %T\n", s)
		}
	}

	interp := interpreter.New()
	interp.SetFile(file)
	if port > 0 {
		interp.SetPort(port)
	}
	return interp.Run(prog)
}

// runWatched re-runs the file in a child process whenever it changes on disk.
// We re-exec the same binary so any state inside the interpreter is reset.
func runWatched(file string, port int, debug bool) {
	bin, err := os.Executable()
	if err != nil {
		fatal("cannot resolve executable: %v", err)
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		fatal("cannot resolve file: %v", err)
	}

	fmt.Printf("%s[mx watch]%s watching %s — press Ctrl+C to stop\n", cYellow, cReset, abs)

	dir := filepath.Dir(abs)
	var lastHash [32]byte
	var cmd *exec.Cmd

	startProcess := func() {
		args := []string{"run", abs}
		if port > 0 {
			args = append(args, "--port", strconv.Itoa(port))
		}
		if debug {
			args = append(args, "--debug")
		}
		cmd = exec.Command(bin, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "%sstart error:%s %v\n", cRed, cReset, err)
		}
	}

	stopProcess := func() {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}

	startProcess()

	for {
		time.Sleep(500 * time.Millisecond)
		hash, err := dirHash(dir)
		if err != nil {
			continue
		}
		if hash != lastHash {
			lastHash = hash
			fmt.Printf("\n%s[mx watch]%s change detected — restarting\n", cYellow, cReset)
			stopProcess()
			startProcess()
		}
	}
}

func dirHash(dir string) ([32]byte, error) {
	h := sha256.New()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".mx") {
			return nil
		}
		fmt.Fprintf(h, "%s|%d|%d\n", path, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		var z [32]byte
		return z, err
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// ===== mx init =====

func cmdInit(args []string) {
	name := "my-mx-app"
	if len(args) > 0 && args[0] != "" {
		name = args[0]
	}
	if err := os.MkdirAll(name, 0o755); err != nil {
		fatal("cannot create %s: %v", name, err)
	}
	files := map[string]string{
		"app.mx":     starterApp,
		".env":       starterEnv,
		"README.md":  fmt.Sprintf("# %s\n\nBuilt with [MX Script](https://github.com/jlkdevelop/mxscript).\n\n## Run\n\n```\nmx run app.mx\n```\n", name),
		".gitignore": ".env\n*.bin\n",
	}
	for f, content := range files {
		path := filepath.Join(name, f)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			fatal("cannot write %s: %v", path, err)
		}
	}
	fmt.Printf("%s✓%s scaffolded %s/\n", cGreen, cReset, name)
	fmt.Printf("\n  cd %s\n  mx run app.mx\n\n", name)
}

const starterApp = `// app.mx — generated by ` + "`mx init`" + `
server {
  port: 8080
}

let appName = "My MX Script App"

route GET / {
  return json({
    message: "Welcome to " + appName,
    docs: "https://github.com/jlkdevelop/mxscript"
  })
}

route GET /hello/:name {
  return json({ greeting: "Hello, " + request.params.name })
}
`

const starterEnv = `# Environment variables — read with env("KEY") inside .mx
# OPENAI_API_KEY=sk-...
`

// ===== mx repl =====

func cmdRepl(args []string) {
	debug := false
	for _, a := range args {
		if a == "--debug" {
			debug = true
		}
	}

	interp := interpreter.New()
	interp.SetFile("<repl>")

	fmt.Printf("%sMX Script%s %s · interactive REPL\n", cGreen, cReset, Version)
	fmt.Printf("%sType%s %s.help%s for help, %s.exit%s or Ctrl-D to leave.\n\n",
		cGray, cReset, cCyan, cReset, cCyan, cReset)

	in := newReplReader()
	var buf strings.Builder
	prompt := func() string {
		if buf.Len() == 0 {
			return cGreen + "mx> " + cReset
		}
		return cGray + "..> " + cReset
	}
	for {
		fmt.Print(prompt())
		line, ok := in.ReadLine()
		if !ok {
			fmt.Println()
			return
		}
		trimmed := strings.TrimSpace(line)

		if buf.Len() == 0 {
			switch trimmed {
			case ".exit", ".quit":
				return
			case ".help":
				fmt.Println("  .exit / .quit — leave the REPL")
				fmt.Println("  .help         — show this help")
				fmt.Println("  .clear        — discard current multi-line input")
				fmt.Println("  .vars         — list defined variables")
				continue
			case ".clear":
				buf.Reset()
				continue
			case ".vars":
				printGlobals(interp)
				continue
			case "":
				continue
			}
		}

		buf.WriteString(line)
		buf.WriteByte('\n')

		// Try to parse what we have so far. If it's incomplete (unbalanced
		// braces / unterminated string), keep reading.
		src := buf.String()
		if !looksComplete(src) {
			continue
		}

		tokens, err := lexer.New(src).Tokenize()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%serror:%s %v\n", cRed, cReset, err)
			buf.Reset()
			continue
		}
		prog, err := parser.New(tokens).Parse()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%serror:%s %v\n", cRed, cReset, err)
			buf.Reset()
			continue
		}
		if debug {
			fmt.Fprintln(os.Stderr, cGray+"--- AST ---"+cReset)
			for _, s := range prog.Stmts {
				fmt.Fprintf(os.Stderr, "  %T\n", s)
			}
		}
		v, err := interp.Exec(prog)
		buf.Reset()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%serror:%s %v\n", cRed, cReset, err)
			continue
		}
		if v.Kind != interpreter.KindNull {
			fmt.Printf("%s=>%s %s\n", cCyan, cReset, interpreter.DisplayValue(v))
		}
	}
}

// looksComplete is a heuristic: balanced braces / parens / brackets and no
// unterminated string. It lets the REPL accept multi-line input.
func looksComplete(src string) bool {
	depth := 0
	inStr := false
	var quote rune
	runes := []rune(src)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if inStr {
			if c == '\\' && i+1 < len(runes) {
				i++
				continue
			}
			if c == quote {
				inStr = false
			}
			continue
		}
		switch c {
		case '"', '\'':
			inStr = true
			quote = c
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			depth--
		}
	}
	return !inStr && depth <= 0
}

func printGlobals(interp *interpreter.Interpreter) {
	g := interp.Globals()
	keys := globalUserKeys(g)
	if len(keys) == 0 {
		fmt.Println("  (no user variables yet)")
		return
	}
	for _, k := range keys {
		v, _ := g.Get(k)
		fmt.Printf("  %s = %s\n", k, interpreter.DisplayValue(v))
	}
}

// globalUserKeys filters out the built-ins so .vars only shows what the user
// has defined.
func globalUserKeys(g *interpreter.Env) []string {
	all := g.Keys()
	out := make([]string, 0, len(all))
	for _, k := range all {
		if interpreter.IsBuiltin(k) {
			continue
		}
		out = append(out, k)
	}
	return out
}

// ===== mx lsp =====

func cmdLSP(args []string) {
	_ = args
	if err := lsp.Run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "%slsp:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}
}

// ===== mx fmt =====

func cmdFmt(args []string) {
	check := false
	write := false
	var paths []string
	for _, a := range args {
		switch a {
		case "--check":
			check = true
		case "-w", "--write":
			write = true
		default:
			paths = append(paths, a)
		}
	}

	// No paths -> read stdin, write to stdout.
	if len(paths) == 0 {
		buf, err := os.ReadFile("/dev/stdin")
		if err != nil {
			fatal("cannot read stdin: %v", err)
		}
		out, err := formatter.Format(string(buf))
		if err != nil {
			printError("<stdin>", err)
			os.Exit(1)
		}
		fmt.Print(out)
		return
	}

	files, err := expandFmtPaths(paths)
	if err != nil {
		fatal("%v", err)
	}
	hadDiff := false
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%scannot read %s:%s %v\n", cRed, file, cReset, err)
			os.Exit(1)
		}
		out, err := formatter.Format(string(src))
		if err != nil {
			printError(file, err)
			os.Exit(1)
		}
		switch {
		case check:
			if string(src) != out {
				fmt.Println(file)
				hadDiff = true
			}
		case write:
			if string(src) != out {
				if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
					fatal("cannot write %s: %v", file, err)
				}
				fmt.Printf("%s✓%s formatted %s\n", cGreen, cReset, file)
			}
		default:
			fmt.Print(out)
		}
	}
	if check && hadDiff {
		fmt.Fprintf(os.Stderr, "\n%sfiles above are not formatted — run `mx fmt -w <path>`%s\n", cYellow, cReset)
		os.Exit(1)
	}
}

func expandFmtPaths(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			err := filepath.Walk(p, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if fi.IsDir() {
					if strings.HasPrefix(fi.Name(), ".") && path != p {
						return filepath.SkipDir
					}
					return nil
				}
				if strings.HasSuffix(path, ".mx") {
					out = append(out, path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else {
			out = append(out, p)
		}
	}
	return out, nil
}

// ===== mx bench =====

// cmdBench runs every `bench_*` function in `*_bench.mx` files and prints
// a summary: ops/sec, ns/op, allocations not measured (Go-style would be
// nice but we don't have allocation hooks in the interpreter yet).
//
//	fn bench_json_encode() {
//	  json_stringify({ id: 1, name: "Jassim", scores: [10, 20, 30] })
//	}
//
//	$ mx bench
//	bench_json_encode    50000 ops    14.2 us/op
func cmdBench(args []string) {
	root := "."
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			root = a
		}
	}

	files, err := findBenchFiles(root)
	if err != nil {
		fatal("bench discovery failed: %v", err)
	}
	if len(files) == 0 {
		fmt.Printf("%sno *_bench.mx files found under %s%s\n", cYellow, root, cReset)
		return
	}

	for _, file := range files {
		fmt.Printf("\n%s%s%s\n", cBold, file, cReset)
		src, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%scannot read %s:%s %v\n", cRed, file, cReset, err)
			continue
		}
		tokens, err := lexer.New(string(src)).Tokenize()
		if err != nil {
			printError(file, err)
			continue
		}
		prog, err := parser.New(tokens).Parse()
		if err != nil {
			printError(file, err)
			continue
		}
		var names []string
		for _, s := range prog.Stmts {
			if fn, ok := s.(*parser.FnDecl); ok && strings.HasPrefix(fn.Name, "bench_") {
				names = append(names, fn.Name)
			}
		}
		if len(names) == 0 {
			fmt.Printf("  %s(no bench_* functions in this file)%s\n", cGray, cReset)
			continue
		}
		for _, name := range names {
			interp := interpreter.New()
			interp.SetFile(file)
			if err := runProgramQuietly(interp, prog); err != nil {
				fmt.Printf("  %s✗%s %s — %v\n", cRed, cReset, name, err)
				continue
			}
			runBench(interp, name)
		}
	}
}

func findBenchFiles(root string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_bench.mx") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// runBench calls fn enough times to fill at least 1 second, doubling
// the iteration count each warm-up round. Reports ops, ops/sec, ns/op.
func runBench(interp *interpreter.Interpreter, name string) {
	const target = 1 * time.Second
	n := 1
	var elapsed time.Duration
	for {
		start := time.Now()
		for i := 0; i < n; i++ {
			if _, err := interp.CallByName(name, nil); err != nil {
				fmt.Printf("  %s✗%s %s — %v\n", cRed, cReset, prettyBenchName(name), err)
				return
			}
		}
		elapsed = time.Since(start)
		if elapsed >= target || n >= 1<<24 {
			break
		}
		// Aim for `target` total — multiply n by the ratio plus a little headroom.
		ratio := float64(target) / float64(elapsed)
		next := int(float64(n) * ratio * 1.2)
		if next <= n {
			next = n * 2
		}
		n = next
	}
	nsPerOp := float64(elapsed.Nanoseconds()) / float64(n)
	opsPerSec := float64(n) / elapsed.Seconds()
	fmt.Printf("  %s%-32s%s %s%9d ops%s   %s%.2f us/op%s   %s(%.0f ops/s)%s\n",
		cBold, prettyBenchName(name), cReset,
		cYellow, n, cReset,
		cCyan, nsPerOp/1000, cReset,
		cGray, opsPerSec, cReset,
	)
}

func prettyBenchName(name string) string {
	stripped := strings.TrimPrefix(name, "bench_")
	return strings.ReplaceAll(stripped, "_", " ")
}

// ===== mx test =====

func cmdTest(args []string) {
	root := "."
	cover := false
	for _, a := range args {
		switch {
		case a == "--cover":
			cover = true
		case !strings.HasPrefix(a, "-"):
			root = a
		}
	}

	files, err := findTestFiles(root)
	if err != nil {
		fatal("test discovery failed: %v", err)
	}
	if len(files) == 0 {
		fmt.Printf("%sno *_test.mx files found under %s%s\n", cYellow, root, cReset)
		return
	}

	totalPass, totalFail := 0, 0
	start := time.Now()
	for _, file := range files {
		fmt.Printf("\n%s%s%s\n", cBold, file, cReset)

		src, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("  %sread error:%s %v\n", cRed, cReset, err)
			totalFail++
			continue
		}
		tokens, err := lexer.New(string(src)).Tokenize()
		if err != nil {
			printError(file, err)
			totalFail++
			continue
		}
		prog, err := parser.New(tokens).Parse()
		if err != nil {
			printError(file, err)
			totalFail++
			continue
		}

		// Discover test_* functions by walking top-level FnDecls.
		var names []string
		for _, s := range prog.Stmts {
			if fn, ok := s.(*parser.FnDecl); ok && strings.HasPrefix(fn.Name, "test_") {
				names = append(names, fn.Name)
			}
		}
		if len(names) == 0 {
			fmt.Printf("  %s(no test_* functions in this file)%s\n", cGray, cReset)
			continue
		}

		// Aggregate coverage across all tests in this file.
		var fileCov *interpreter.Coverage
		// Each test gets a fresh interpreter so state can't leak between tests.
		for _, name := range names {
			interp := interpreter.New()
			interp.SetFile(file)
			if cover {
				cov := interp.EnableCoverage()
				if fileCov == nil {
					fileCov = cov
				}
				_ = fileCov
			}
			if err := runProgramQuietly(interp, prog); err != nil {
				fmt.Printf("  %s✗%s %s — %v\n", cRed, cReset, prettyTestName(name), err)
				totalFail++
				continue
			}
			_, err := interp.CallByName(name, nil)
			if err != nil {
				fmt.Printf("  %s✗%s %s — %v\n", cRed, cReset, prettyTestName(name), err)
				totalFail++
			} else {
				fmt.Printf("  %s✓%s %s\n", cGreen, cReset, prettyTestName(name))
				totalPass++
			}
			// Merge this test's hits into the file-level coverage.
			if cover && fileCov != interp.Coverage() {
				for _, ln := range interp.Coverage().ExecutedLines() {
					fileCov.Hit(ln)
				}
			}
		}

		if cover {
			executable := parser.ExecutableLines(prog)
			covered := 0
			ranSet := map[int]bool{}
			for _, ln := range fileCov.ExecutedLines() {
				ranSet[ln] = true
			}
			for ln := range executable {
				if ranSet[ln] {
					covered++
				}
			}
			pct := 100.0
			if len(executable) > 0 {
				pct = float64(covered) * 100.0 / float64(len(executable))
			}
			fmt.Printf("  %scoverage:%s %d/%d lines (%.1f%%)\n",
				cGray, cReset, covered, len(executable), pct)
		}
	}

	elapsed := time.Since(start).Round(time.Millisecond)
	fmt.Println()
	if totalFail == 0 {
		fmt.Printf("%s✓ %d passed%s in %s\n", cGreen, totalPass, cReset, elapsed)
	} else {
		fmt.Printf("%s✗ %d failed, %d passed%s in %s\n",
			cRed, totalFail, totalPass, cReset, elapsed)
		os.Exit(1)
	}
}

// runProgramQuietly executes top-level statements (let/fn/etc) but skips
// route registration and HTTP server startup — tests don't need a server.
func runProgramQuietly(interp *interpreter.Interpreter, prog *parser.Program) error {
	_, err := interp.Exec(prog)
	return err
}

func findTestFiles(root string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_test.mx") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func prettyTestName(name string) string {
	stripped := strings.TrimPrefix(name, "test_")
	return strings.ReplaceAll(stripped, "_", " ")
}

// ===== mx build =====

func cmdBuild(args []string) {
	vercel := false
	var file string
	for _, a := range args {
		switch a {
		case "--vercel":
			vercel = true
		default:
			if file == "" {
				file = a
			}
		}
	}
	if file == "" {
		file = "app.mx"
	}

	src, err := os.ReadFile(file)
	if err != nil {
		fatal("cannot read %s: %v", file, err)
	}
	tokens, err := lexer.New(string(src)).Tokenize()
	if err != nil {
		printError(file, err)
		os.Exit(1)
	}
	if _, err := parser.New(tokens).Parse(); err != nil {
		printError(file, err)
		os.Exit(1)
	}

	if vercel {
		cmdBuildVercel(file)
		return
	}
	fmt.Printf("%s✓%s %s parses cleanly\n", cGreen, cReset, file)
}

// cmdBuildVercel emits a Vercel-deployable Go project that embeds the user's
// .mx source and serves it via the interpreter library. Vercel's Go framework
// preset auto-detects the generated go.mod + main.go and runs the binary on
// the platform-provided $PORT.
//
// Files written (in the current working directory):
//   - main.go      — embeds the .mx file and starts an http.Server
//   - go.mod       — pins the mxscript runtime to the current CLI version
//   - vercel.json  — declares the framework preset
//
// The generated files are safe to commit. Re-run `mx build --vercel` whenever
// you upgrade the mx CLI to refresh the pinned runtime version.
func cmdBuildVercel(file string) {
	// The generator embeds the source via //go:embed using the user's relative
	// path. main.go must be in (or above) a directory containing the .mx file.
	// For the common one-file-at-root case, this works out of the box.
	embedPath := filepath.ToSlash(file)

	mainGo := fmt.Sprintf(vercelMainTemplate, embedPath, embedPath)
	goMod := fmt.Sprintf(vercelGoModTemplate, Version)
	vercelJSON := vercelJSONTemplate

	writes := []struct {
		path    string
		content string
	}{
		{"main.go", mainGo},
		{"go.mod", goMod},
		{"vercel.json", vercelJSON},
	}

	for _, w := range writes {
		if _, err := os.Stat(w.path); err == nil {
			fmt.Printf("%s!%s %s already exists — overwriting\n", cYellow, cReset, w.path)
		}
		if err := os.WriteFile(w.path, []byte(w.content), 0o644); err != nil {
			fatal("cannot write %s: %v", w.path, err)
		}
		fmt.Printf("%s✓%s wrote %s\n", cGreen, cReset, w.path)
	}

	fmt.Println()
	fmt.Printf("%sNext:%s\n", cBold, cReset)
	fmt.Println("  1. git add main.go go.mod vercel.json")
	fmt.Println("  2. git commit -m \"Deploy via mx build --vercel\"")
	fmt.Println("  3. git push  (Vercel autodeploys on push)")
	fmt.Println()
	fmt.Printf("%sOr deploy directly:%s  vercel deploy --prod\n", cGray, cReset)
}

// vercelMainTemplate is the generated Go entrypoint. It embeds the .mx source
// at compile time, lexes + parses + loads it via the interpreter library,
// then serves the resulting handler on $PORT (Vercel's convention).
//
// Two %s slots: both the //go:embed directive and the SetFile path, both
// receiving the user's .mx filename.
const vercelMainTemplate = `// Code generated by ` + "`mx build --vercel`" + `. DO NOT EDIT.
package main

import (
	_ "embed"
	"log"
	"net/http"
	"os"

	"github.com/jlkdevelop/mxscript/interpreter"
	"github.com/jlkdevelop/mxscript/lexer"
	"github.com/jlkdevelop/mxscript/parser"
)

//go:embed %s
var source string

func main() {
	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		log.Fatalf("mx lex: %%v", err)
	}
	prog, err := parser.New(tokens).Parse()
	if err != nil {
		log.Fatalf("mx parse: %%v", err)
	}

	interp := interpreter.New()
	interp.SetFile(%q)
	if err := interp.Load(prog); err != nil {
		log.Fatalf("mx load: %%v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("MX Script (vercel) listening on %%s", addr)
	if err := http.ListenAndServe(addr, interp.Handler()); err != nil {
		log.Fatal(err)
	}
}
`

// vercelGoModTemplate pins the mxscript runtime to the version of the CLI
// that generated the build. One %s slot for the version (e.g. "v0.12.0").
const vercelGoModTemplate = `module mxscript-app

go 1.22

require github.com/jlkdevelop/mxscript %s
`

// vercelJSONTemplate enables Vercel's Go framework preset. Without this,
// Vercel may misdetect the project type and fail the build.
const vercelJSONTemplate = `{
  "$schema": "https://openapi.vercel.sh/vercel.json",
  "framework": "go"
}
`

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%serror:%s "+format+"\n", append([]any{cRed, cReset}, args...)...)
	os.Exit(1)
}
