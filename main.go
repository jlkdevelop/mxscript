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
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jlkdevelop/mxscript/checker"
	"github.com/jlkdevelop/mxscript/formatter"
	"github.com/jlkdevelop/mxscript/interpreter"
	"github.com/jlkdevelop/mxscript/lexer"
	"github.com/jlkdevelop/mxscript/lsp"
	"github.com/jlkdevelop/mxscript/parser"
	mxpkg "github.com/jlkdevelop/mxscript/pkg"
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
var Version = "v0.65.0"

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
	// Wire the package-path resolver so `import "github.com/foo/bar"`
	// in user code goes through mx_modules. Without this hook, the
	// interpreter would treat package paths as relative files and
	// fail to find them.
	interpreter.PackageResolver = func(importPath string) string {
		return mxpkg.ResolveImportFile(".", importPath)
	}

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
	case "new":
		cmdNew(args)
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
	case "upgrade":
		cmdUpgrade(args)
	case "doctor":
		cmdDoctor(args)
	case "routes":
		cmdRoutes(args)
	case "check":
		cmdCheck(args)
	case "pkg":
		cmdPkg(args)
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
	fmt.Println("  init [name]           Scaffold a new MX Script project (minimal)")
	fmt.Println("  new <template> [name] Scaffold from template: todo|chat|ai|blog|api")
	fmt.Println("  build <file.mx>       Type-check & validate an MX Script file")
	fmt.Println("  build --vercel        Generate a Vercel-deployable Go project from app.mx")
	fmt.Println("  build --wasm          Compile the interpreter to dist/mx.wasm + JS shim (browser playground)")
	fmt.Println("  repl                  Start an interactive REPL")
	fmt.Println("  test [path]           Run *_test.mx files (default: current dir)")
	fmt.Println("  bench [path]          Run *_bench.mx benchmarks (each fn bench_*)")
	fmt.Println("  fmt [paths]           Format .mx files (-w writes, --check exits 1 on diffs)")
	fmt.Println("  lsp                   Run the Language Server (JSON-RPC over stdio)")
	fmt.Println("  upgrade               Self-update to the latest release")
	fmt.Println("  doctor                Diagnose env / install / runtime")
	fmt.Println("  routes <file.mx>      List every route the program registers (no server boot)")
	fmt.Println("  check <file.mx>       Static analysis: undefined idents, wrong arity, unused lets")
	fmt.Println("  pkg <init|add|list|update|remove|install> [args]")
	fmt.Println("  version               Print version and exit")
	fmt.Println("  help                  Show this help")
	fmt.Println()
	fmt.Println("Flags for `run`:")
	fmt.Println("  --port N              Override server.port (default 8080)")
	fmt.Println("  --watch               Restart on file changes (hot reload)")
	fmt.Println("  --debug               Print tokens, AST, and verbose errors")
	fmt.Println()
	fmt.Printf("%sCreated by Jassim Alkharafi · github.com/jlkdevelop/mxscript%s\n", cGray, cReset)
}

// ===== mx run =====

func cmdRun(args []string) {
	var file string
	var eval string
	port := 0
	watch := false
	debug := false
	bytecode := false

	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--bytecode":
			bytecode = true
			i++
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
		if err := runSource("<eval>", []byte(eval), port, debug, bytecode); err != nil {
			printError("<eval>", err)
			os.Exit(1)
		}
		return
	}

	if file == "" {
		fatal("usage: mx run <file.mx> | mx run --eval '<snippet>'")
	}

	if watch {
		runWatched(file, port, debug, bytecode)
		return
	}

	if err := runOnce(file, port, debug, bytecode); err != nil {
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

func runOnce(file string, port int, debug, bytecode bool) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", file, err)
	}
	return runSource(file, src, port, debug, bytecode)
}

func runSource(file string, src []byte, port int, debug, bytecode bool) error {
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
	if bytecode {
		interp.SetBytecode(true)
	}
	return interp.Run(prog)
}

// runWatched re-runs the file in a child process whenever it changes on disk.
// We re-exec the same binary so any state inside the interpreter is reset.
func runWatched(file string, port int, debug, bytecode bool) {
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
		if bytecode {
			args = append(args, "--bytecode")
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
		".gitignore": ".env\n*.bin\nmx_modules/\n",
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
	bytecode := false
	for _, a := range args {
		switch {
		case a == "--bytecode":
			bytecode = true
		case !strings.HasPrefix(a, "-"):
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
			if bytecode {
				interp.SetBytecode(true)
			}
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

// ===== mx upgrade =====
//
// Pulls the latest release tag from the GitHub API, downloads the
// matching archive for the current OS / arch, extracts the `mx` binary
// (or `mx.exe` on Windows), and atomically swaps it for the running
// executable.
//
// Skips if you're already on the newest release. `--force` re-downloads
// regardless. Behind the scenes this hits `api.github.com/repos/...`
// (no auth required for public repos).

func cmdUpgrade(args []string) {
	force := false
	for _, a := range args {
		if a == "--force" || a == "-f" {
			force = true
		}
	}

	fmt.Printf("%scurrent:%s %s\n", cGray, cReset, Version)
	fmt.Printf("%schecking github.com/jlkdevelop/mxscript for newer release…%s\n", cGray, cReset)

	resp, err := http.Get("https://api.github.com/repos/jlkdevelop/mxscript/releases/latest")
	if err != nil {
		fatal("cannot reach GitHub: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fatal("GitHub returned %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name        string `json:"name"`
			DownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		fatal("cannot parse release JSON: %v", err)
	}
	if !force && rel.TagName == Version {
		fmt.Printf("%s✓%s already on the latest release (%s)\n", cGreen, cReset, rel.TagName)
		return
	}
	fmt.Printf("%slatest:%s %s\n", cGray, cReset, rel.TagName)

	// Match an asset for our os/arch — GoReleaser uses names like
	// mx_v0.42.0_darwin_arm64.tar.gz / mx_v0.42.0_windows_amd64.zip.
	want := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	var asset struct{ Name, URL string }
	for _, a := range rel.Assets {
		if !strings.Contains(a.Name, want) {
			continue
		}
		if strings.HasSuffix(a.Name, ".tar.gz") || strings.HasSuffix(a.Name, ".zip") {
			asset.Name = a.Name
			asset.URL = a.DownloadURL
			break
		}
	}
	if asset.URL == "" {
		fatal("no release asset for %s/%s — please install manually", runtime.GOOS, runtime.GOARCH)
	}

	fmt.Printf("%sdownloading %s…%s\n", cGray, asset.Name, cReset)
	dl, err := http.Get(asset.URL)
	if err != nil {
		fatal("download failed: %v", err)
	}
	defer dl.Body.Close()

	tmpBin, err := extractMXBinary(dl.Body, asset.Name)
	if err != nil {
		fatal("extract failed: %v", err)
	}
	defer os.Remove(tmpBin)

	cur, err := os.Executable()
	if err != nil {
		fatal("can't resolve own path: %v", err)
	}
	if err := os.Chmod(tmpBin, 0o755); err != nil {
		fatal("chmod: %v", err)
	}
	if err := os.Rename(tmpBin, cur); err != nil {
		// Some filesystems disallow cross-device rename; fall back to copy.
		if cpErr := copyFile(tmpBin, cur); cpErr != nil {
			fatal("install failed (rename: %v, copy: %v)", err, cpErr)
		}
	}
	fmt.Printf("%s✓%s upgraded %s → %s\n", cGreen, cReset, Version, rel.TagName)
}

// extractMXBinary pulls `mx` (or `mx.exe`) out of the GoReleaser archive,
// streaming it to a temp file. Returns the temp path on success.
func extractMXBinary(r io.Reader, archiveName string) (string, error) {
	tmp, err := os.CreateTemp("", "mx-upgrade-*")
	if err != nil {
		return "", err
	}
	tmp.Close()
	tmpPath := tmp.Name()

	binaryName := "mx"
	if runtime.GOOS == "windows" {
		binaryName = "mx.exe"
	}

	if strings.HasSuffix(archiveName, ".zip") {
		// We need a seekable reader for the zip package; buffer to disk.
		buf, err := os.CreateTemp("", "mx-zip-*.zip")
		if err != nil {
			return "", err
		}
		defer os.Remove(buf.Name())
		if _, err := io.Copy(buf, r); err != nil {
			buf.Close()
			return "", err
		}
		buf.Close()
		zr, err := zip.OpenReader(buf.Name())
		if err != nil {
			return "", err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if filepath.Base(f.Name) == binaryName {
				rc, err := f.Open()
				if err != nil {
					return "", err
				}
				defer rc.Close()
				out, err := os.Create(tmpPath)
				if err != nil {
					return "", err
				}
				_, err = io.Copy(out, rc)
				out.Close()
				return tmpPath, err
			}
		}
		return "", errors.New("binary not found in zip")
	}

	// .tar.gz path
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return "", err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return "", errors.New("binary not found in tar.gz")
		}
		if err != nil {
			return "", err
		}
		if filepath.Base(h.Name) == binaryName {
			out, err := os.Create(tmpPath)
			if err != nil {
				return "", err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			return tmpPath, err
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// ===== mx doctor =====
//
// Quick env / install diagnostics. Useful for new users who've hit
// something weird and want a one-line "is anything obviously wrong?"
// view. Prints version, paths, runtime info, network reachability.

func cmdDoctor(args []string) {
	exe, _ := os.Executable()
	pwd, _ := os.Getwd()

	fmt.Printf("\n%sMX Script doctor%s\n", cBold, cReset)
	fmt.Printf("  %s%-20s%s %s\n", cGray, "version:", cReset, Version)
	fmt.Printf("  %s%-20s%s %s\n", cGray, "binary:", cReset, exe)
	fmt.Printf("  %s%-20s%s %s/%s (Go %s)\n", cGray, "platform:", cReset, runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Printf("  %s%-20s%s %s\n", cGray, "cwd:", cReset, pwd)

	// Env keys MX commonly cares about.
	fmt.Printf("\n%senv:%s\n", cBold, cReset)
	for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "JWT_SECRET", "SESSION_SECRET", "DATABASE_URL", "PORT"} {
		v := os.Getenv(key)
		mark := cGray + "—" + cReset
		val := mark
		if v != "" {
			val = fmt.Sprintf("%sset%s (%d chars)", cGreen, cReset, len(v))
		}
		fmt.Printf("  %s%-22s%s %s\n", cGray, key+":", cReset, val)
	}

	// Reachability checks (quick, parallelizable).
	fmt.Printf("\n%snetwork:%s\n", cBold, cReset)
	checks := []struct{ Name, URL string }{
		{"GitHub releases", "https://api.github.com/repos/jlkdevelop/mxscript/releases/latest"},
		{"OpenAI", "https://api.openai.com"},
	}
	for _, c := range checks {
		client := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequest("HEAD", c.URL, nil)
		start := time.Now()
		resp, err := client.Do(req)
		elapsed := time.Since(start).Round(time.Millisecond)
		mark := cGreen + "✓"
		detail := fmt.Sprintf("%dms", elapsed.Milliseconds())
		if err != nil || resp.StatusCode >= 500 {
			mark = cRed + "✗"
			if err != nil {
				detail = err.Error()
			} else {
				detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
		}
		if resp != nil {
			resp.Body.Close()
		}
		fmt.Printf("  %s%s%s %-20s %s\n", mark, cReset, "", c.Name, detail)
	}
	fmt.Println()
}

// ===== mx check =====

// cmdCheck runs the static analyzer over a single file and prints
// diagnostics. Exits 1 if any errors are found (warnings don't fail
// the build) so it composes naturally with CI.
//
//	$ mx check app.mx
//	app.mx:42:7: error: undefined identifier "respnse"
//	app.mx:51:5: warning: unused let binding "tmp"
//	2 issues (1 error, 1 warning)
func cmdCheck(args []string) {
	if len(args) < 1 {
		fatal("usage: mx check <file.mx>")
	}
	file := args[0]
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("cannot read %s: %v", file, err)
	}
	tokens, err := lexer.New(string(src)).Tokenize()
	if err != nil {
		printError(file, err)
		os.Exit(1)
	}
	prog, err := parser.New(tokens).Parse()
	if err != nil {
		printError(file, err)
		os.Exit(1)
	}
	diags := checker.Check(prog)
	errors, warnings := 0, 0
	for _, d := range diags {
		color := cRed
		if d.Severity == checker.SeverityWarning {
			color = cYellow
			warnings++
		} else {
			errors++
		}
		fmt.Fprintf(os.Stderr, "%s:%d:%d: %s%s%s: %s\n",
			file, d.Line, d.Col, color, d.Severity, cReset, d.Message)
	}
	if len(diags) == 0 {
		fmt.Printf("%s✓%s no issues in %s\n", cGreen, cReset, file)
		return
	}
	fmt.Fprintf(os.Stderr, "\n%d issue", len(diags))
	if len(diags) != 1 {
		fmt.Fprint(os.Stderr, "s")
	}
	fmt.Fprintf(os.Stderr, " (%d error", errors)
	if errors != 1 {
		fmt.Fprint(os.Stderr, "s")
	}
	fmt.Fprintf(os.Stderr, ", %d warning", warnings)
	if warnings != 1 {
		fmt.Fprint(os.Stderr, "s")
	}
	fmt.Fprintln(os.Stderr, ")")
	if errors > 0 {
		os.Exit(1)
	}
}

// ===== mx build --wasm =====

// cmdBuildWasm shells out to `go build` with GOOS=js GOARCH=wasm to
// produce a browser-runnable copy of the MX interpreter, then copies
// the matching wasm_exec.js shim from $GOROOT into dist/. The caller
// can serve dist/ from any static host and call window.mxRun(source).
//
//	$ mx build --wasm
//	dist/mx.wasm           15M  (interpreter compiled to wasm)
//	dist/wasm_exec.js      26K  (Go's standard JS host shim)
//
// The wasm build excludes SQLite, Redis, and the durable-jobs queue
// (see interpreter/sql_wasm.go etc.) — those depend on TCP and
// libc-style shims the browser doesn't provide. Routes still parse
// and register, they just never serve traffic.
func cmdBuildWasm() {
	if err := os.MkdirAll("dist", 0o755); err != nil {
		fatal("cannot create dist/: %v", err)
	}
	out := filepath.Join("dist", "mx.wasm")

	// Locate cmd/mxwasm relative to the executable's source tree. We
	// resolve via `go env GOMOD` so this works from any working dir
	// inside the repo.
	cmd := exec.Command("go", "build", "-o", out, "./cmd/mxwasm/")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("%scompiling%s mx.wasm (this takes 10-15s)...\n", cYellow, cReset)
	if err := cmd.Run(); err != nil {
		fatal("go build --wasm failed: %v", err)
	}

	// Copy the matching wasm_exec.js. Go ships it with the toolchain;
	// the path moved between Go versions, so probe both locations.
	gorootCmd := exec.Command("go", "env", "GOROOT")
	gorootRaw, err := gorootCmd.Output()
	if err != nil {
		fatal("cannot resolve GOROOT: %v", err)
	}
	goroot := strings.TrimSpace(string(gorootRaw))
	candidates := []string{
		filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"),
		filepath.Join(goroot, "misc", "wasm", "wasm_exec.js"),
	}
	var src string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			src = c
			break
		}
	}
	if src == "" {
		fatal("could not find wasm_exec.js in %s", goroot)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		fatal("cannot read wasm_exec.js: %v", err)
	}
	if err := os.WriteFile(filepath.Join("dist", "wasm_exec.js"), raw, 0o644); err != nil {
		fatal("cannot write dist/wasm_exec.js: %v", err)
	}

	info, _ := os.Stat(out)
	size := "?"
	if info != nil {
		size = fmt.Sprintf("%.1f MB", float64(info.Size())/(1024*1024))
	}
	fmt.Printf("%s✓%s wrote dist/mx.wasm (%s) and dist/wasm_exec.js\n", cGreen, cReset, size)
	fmt.Println("\nServe dist/ and load both files in an HTML page that calls window.mxRun(source).")
	fmt.Println("See site/playground/index.html for a working example.")
}

// ===== mx pkg =====

// cmdPkg dispatches to one of the package-manager subcommands. The
// model: each project has a `mxpkg.json` manifest at its root and
// a `mx_modules/` directory holding cloned dependencies.
//
//	mx pkg init                  scaffold mxpkg.json
//	mx pkg add <import-path>     git clone + lock to current SHA
//	mx pkg list                  print manifest deps
//	mx pkg remove <import-path>  delete on disk + manifest entry
//	mx pkg update [<path>]       git pull + re-lock SHA (all or one)
//	mx pkg install               clone every manifest dep at locked SHA
func cmdPkg(args []string) {
	if len(args) < 1 {
		fatal("usage: mx pkg <init|add|list|update|remove|install> [args]")
	}
	sub, rest := args[0], args[1:]
	dir := "."
	switch sub {
	case "init":
		name := filepath.Base(mustAbs(dir))
		if len(rest) > 0 {
			name = rest[0]
		}
		created, err := mxpkg.Init(dir, name)
		if err != nil {
			fatal("pkg init: %v", err)
		}
		if !created {
			fmt.Printf("%s%s%s already exists\n", cYellow, mxpkg.ManifestFile, cReset)
			return
		}
		fmt.Printf("%s✓%s wrote %s\n", cGreen, cReset, mxpkg.ManifestFile)
	case "add":
		if len(rest) < 1 {
			fatal("usage: mx pkg add <import-path>")
		}
		dep, err := mxpkg.Add(dir, rest[0])
		if err != nil {
			fatal("pkg add: %v", err)
		}
		fmt.Printf("%s✓%s added %s @ %s\n", cGreen, cReset, dep.URL, dep.Ref[:12])
	case "list", "ls":
		m, err := mxpkg.LoadManifest(dir)
		if err != nil {
			fatal("pkg list: %v", err)
		}
		if m == nil || len(m.Dependencies) == 0 {
			fmt.Printf("%sno dependencies%s\n", cGray, cReset)
			return
		}
		keys := make([]string, 0, len(m.Dependencies))
		for k := range m.Dependencies {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			d := m.Dependencies[k]
			ref := d.Ref
			if len(ref) > 12 {
				ref = ref[:12]
			}
			fmt.Printf("  %s%s%s %s%s%s\n", cCyan, k, cReset, cGray, ref, cReset)
		}
	case "remove", "rm":
		if len(rest) < 1 {
			fatal("usage: mx pkg remove <import-path>")
		}
		if err := mxpkg.Remove(dir, rest[0]); err != nil {
			fatal("pkg remove: %v", err)
		}
		fmt.Printf("%s✓%s removed %s\n", cGreen, cReset, rest[0])
	case "update":
		target := ""
		if len(rest) > 0 {
			target = rest[0]
		}
		if err := mxpkg.Update(dir, target); err != nil {
			fatal("pkg update: %v", err)
		}
		fmt.Printf("%s✓%s updated\n", cGreen, cReset)
	case "install":
		if err := mxpkg.Install(dir); err != nil {
			fatal("pkg install: %v", err)
		}
		fmt.Printf("%s✓%s install complete\n", cGreen, cReset)
	default:
		fatal("unknown pkg subcommand %q", sub)
	}
}

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// ===== mx routes =====

// cmdRoutes parses and loads a program without booting the HTTP server,
// then prints every registered route. Useful for: understanding an
// unfamiliar codebase, generating an OpenAPI spec offline, or asserting
// in CI that the route set hasn't changed unexpectedly.
//
//	$ mx routes app.mx
//	GET    /                         [auth, log]
//	POST   /api/v1/users
//	GET    /api/v1/users/:id         [auth]
//	5 routes
func cmdRoutes(args []string) {
	if len(args) < 1 {
		fatal("usage: mx routes <file.mx>")
	}
	file := args[0]
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("cannot read %s: %v", file, err)
	}
	tokens, err := lexer.New(string(src)).Tokenize()
	if err != nil {
		printError(file, err)
		os.Exit(1)
	}
	prog, err := parser.New(tokens).Parse()
	if err != nil {
		printError(file, err)
		os.Exit(1)
	}
	interp := interpreter.New()
	interp.SetFile(file)
	if err := interp.Load(prog); err != nil {
		printError(file, err)
		os.Exit(1)
	}

	routes := interp.RouteSummary()
	if len(routes) == 0 {
		fmt.Printf("%sno routes registered in %s%s\n", cYellow, file, cReset)
		return
	}
	for _, r := range routes {
		mw := ""
		if len(r.Middlewares) > 0 {
			mw = fmt.Sprintf("  %s[%s]%s", cGray, strings.Join(r.Middlewares, ", "), cReset)
		}
		fmt.Printf("  %s%-7s%s %s%s\n", cCyan, r.Method, cReset, r.Path, mw)
	}
	noun := "routes"
	if len(routes) == 1 {
		noun = "route"
	}
	fmt.Printf("\n%s%d %s%s\n", cBold, len(routes), noun, cReset)
}

// ===== mx new =====

// cmdNew scaffolds an opinionated starter project. Each template is one
// or more .mx files plus a tiny README, .env.example, and .gitignore.
//
//	mx new todo my-todo
//	mx new chat realtime-app
//	mx new ai my-bot
//	mx new blog my-blog
//	mx new api users-api
func cmdNew(args []string) {
	if len(args) == 0 {
		fatal("usage: mx new <template> [name]\n\nTemplates: todo, chat, ai, blog, api")
	}
	template := args[0]
	name := template + "-app"
	if len(args) > 1 && args[1] != "" {
		name = args[1]
	}

	tpl, ok := projectTemplates[template]
	if !ok {
		fatal("unknown template %q\nAvailable: todo, chat, ai, blog, api", template)
	}

	if err := os.MkdirAll(name, 0o755); err != nil {
		fatal("cannot create %s: %v", name, err)
	}
	for path, content := range tpl.Files {
		full := filepath.Join(name, path)
		if dir := filepath.Dir(full); dir != "" && dir != "." {
			_ = os.MkdirAll(dir, 0o755)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			fatal("cannot write %s: %v", full, err)
		}
	}

	fmt.Printf("\n%s✓%s scaffolded %s%s/%s%s using template %s%s%s\n\n",
		cGreen, cReset, cBold, name, cReset, cGray, cBold, template, cReset)
	fmt.Println(tpl.Hint)
	fmt.Printf("\n  cd %s\n  mx run app.mx\n\n", name)
}

type projectTemplate struct {
	Description string
	Hint        string
	Files       map[string]string
}

var projectTemplates = map[string]projectTemplate{
	"todo": {
		Description: "Full-stack todo API with KV persistence + JWT auth",
		Hint:        "  Set JWT_SECRET in .env and POST /login with { username, password }.",
		Files: map[string]string{
			"app.mx":       starterTodoApp,
			".env.example": "JWT_SECRET=replace-me-with-something-long\n",
			".gitignore":   ".env\n*.bin\n*.db\nmx_modules/\n",
			"README.md":    "# Todo API\n\nBuilt with [MX Script](https://mxscript.com).\n\n```\ncp .env.example .env\nmx run app.mx\n```\n",
		},
	},
	"chat": {
		Description: "Real-time chat with WebSockets + sessions + browser client",
		Hint:        "  Open the URL in two tabs and type messages.",
		Files: map[string]string{
			"app.mx":       starterChatApp,
			".env.example": "CHAT_SECRET=dev-secret-change-me\n",
			".gitignore":   ".env\n*.bin\n",
			"README.md":    "# MX Chat\n\nReal-time chat in 80 lines of MX Script.\n\n```\nmx run app.mx\n```\n",
		},
	},
	"ai": {
		Description: "AI agent with tool calling (3 tools, 5-turn loop)",
		Hint:        "  Set OPENAI_API_KEY in .env, then `mx run app.mx`.",
		Files: map[string]string{
			"app.mx":       starterAIApp,
			".env.example": "OPENAI_API_KEY=sk-...\n",
			".gitignore":   ".env\n*.bin\n",
			"README.md":    "# AI Agent\n\nTool-calling LLM agent in MX Script.\n",
		},
	},
	"blog": {
		Description: "SSR blog with SQLite + markdown posts + admin",
		Hint:        "  Visit /admin to write posts, / to read them.",
		Files: map[string]string{
			"app.mx":       starterBlogApp,
			".env.example": "ADMIN_PASSWORD=admin\nSESSION_SECRET=replace-me\n",
			".gitignore":   ".env\n*.bin\n*.db\nmx_modules/\n",
			"README.md":    "# Blog\n\nMarkdown blog with SQLite backend.\n",
		},
	},
	"api": {
		Description: "REST API with grouped routes + OpenAPI spec + Swagger UI",
		Hint:        "  Visit /docs for the interactive API explorer.",
		Files: map[string]string{
			"app.mx":       starterAPIApp,
			".env.example": "PORT=8080\n",
			".gitignore":   ".env\n*.bin\n*.db\nmx_modules/\n",
			"README.md":    "# REST API\n\nWith built-in OpenAPI spec + Swagger UI.\n",
		},
	},
}

const starterTodoApp = `// Todo API — generated by mx new todo
server { port: 8080, log: true }

let DB = "./todos.db"
let SECRET = env_required("JWT_SECRET")

let db = sql.open(DB)
sql.migrate(db, [
  "CREATE TABLE IF NOT EXISTS todos (id INTEGER PRIMARY KEY, title TEXT, done INTEGER, created_at TEXT)"
])

middleware require_auth {
  let claims = jwt.verify(request.bearer_token, SECRET)
  if (claims == null) {
    return status(401, { error: "unauthorized" })
  }
}

post /login {
  // Demo auth — replace with real user lookup.
  if (request.body?.password != "demo") {
    return status(401, { error: "bad creds" })
  }
  let token = jwt.sign({ sub: request.body.username, exp: now() / 1000 + 86400 }, SECRET)
  return json({ token: token })
}

group /api {
  use require_auth

  get /todos {
    return json(sql.query(db, "SELECT * FROM todos ORDER BY id DESC"))
  }

  post /todos {
    let body = request.body
    let r = validate(body, {
      type: "object",
      required: ["title"],
      properties: { title: { type: "string", min_length: 1 } }
    })
    if (!r.valid) { return status(400, { errors: r.errors }) }
    let res = sql.exec(db, "INSERT INTO todos (title, done, created_at) VALUES (?, 0, ?)", body.title, now_iso())
    return status(201, { id: res.last_insert_id, title: body.title })
  }

  put /todos/:id/done {
    sql.exec(db, "UPDATE todos SET done = 1 WHERE id = ?", num(request.params.id))
    return json({ ok: true })
  }

  delete /todos/:id {
    sql.exec(db, "DELETE FROM todos WHERE id = ?", num(request.params.id))
    return json({ deleted: request.params.id })
  }
}

get / {
  return json({
    app: "Todo API",
    endpoints: ["POST /login", "GET /api/todos", "POST /api/todos", "PUT /api/todos/:id/done", "DELETE /api/todos/:id"]
  })
}
`

const starterChatApp = `// Real-time chat — generated by mx new chat
server { port: 8080, log: true }

let clients = []

fn broadcast(msg) {
  loop clients as c {
    try { c(msg) } catch (e) { }
  }
}

post /login {
  if (request.body?.username == null) {
    return status(400, { error: "username required" })
  }
  return session.create({ username: request.body.username }, {
    secret: env("CHAT_SECRET", "dev-secret"),
    max_age: 86400
  })
}

ws /chat {
  let claims = session.read(request, env("CHAT_SECRET", "dev-secret"))
  if (claims == null) { close(4001, "login first"); return }
  let username = claims.username

  clients = push(clients, send)
  broadcast(json_stringify({ type: "join", username: username }))

  while (true) {
    let raw = recv()
    if (raw == null) { break }
    let msg = try { json_parse(raw) } catch { null }
    if (msg?.text == null) { continue }
    broadcast(json_stringify({ type: "message", username: username, text: msg.text }))
  }

  clients = filter(clients, fn(c) { return c != send })
  broadcast(json_stringify({ type: "leave", username: username }))
}

get / {
  return html("<h1>MX Chat</h1><p>Wire your own client — see examples/chat.mx in the repo for an HTML version.</p>")
}
`

const starterAIApp = `// AI agent — generated by mx new ai
let tools = [
  { name: "now", description: "Current time in ISO 8601",
    params: { type: "object", properties: {} },
    handler: fn(_) { return now_iso() } },
  { name: "calc", description: "Evaluate arithmetic",
    params: { type: "object", properties: { expr: { type: "string" } }, required: ["expr"] },
    handler: fn(args) { return str(num(args.expr)) } }
]

fn agent(question) {
  let messages = [{ role: "user", content: question }]
  loop 5 as turn {
    let r = ai.complete("", { tools: tools, messages: messages })
    if (isString(r)) { print(r); return }
    let assistant = { role: "assistant", content: r.content ?? "", tool_calls: r.tool_calls }
    messages = push(messages, assistant)
    loop r.tool_calls as call {
      let t = find(tools, fn(x) { return x.name == call.name })
      let result = "<unknown>"
      if (t != null) { result = t.handler(call.arguments) }
      messages = push(messages, { role: "tool", tool_call_id: call.id, content: str(result) })
    }
  }
}

agent("What time is it?")
`

const starterBlogApp = `// Blog — generated by mx new blog
server { port: 8080, log: true }

let DB = "./blog.db"
let db = sql.open(DB)
sql.migrate(db, [
  "CREATE TABLE IF NOT EXISTS posts (id INTEGER PRIMARY KEY, slug TEXT UNIQUE, title TEXT, body_md TEXT, created_at TEXT)"
])

middleware require_admin {
  let claims = session.read(request, env("SESSION_SECRET", "dev"))
  if (claims?.role != "admin") {
    return redirect("/admin/login")
  }
}

get / {
  let posts = sql.query(db, "SELECT slug, title, created_at FROM posts ORDER BY id DESC")
  let html_body = "<h1>Blog</h1><ul>"
  loop posts as p {
    html_body = html_body + "<li><a href='/p/" + p.slug + "'>" + html_escape(p.title) + "</a> <small>" + p.created_at + "</small></li>"
  }
  html_body = html_body + "</ul>"
  return html(html_body)
}

get /p/:slug {
  let post = sql.query_one(db, "SELECT * FROM posts WHERE slug = ?", request.params.slug)
  if (post == null) { return status(404, "not found") }
  return html("<article><h1>" + html_escape(post.title) + "</h1>" + markdown(post.body_md) + "</article>")
}

get /admin/login { return html("<form method=POST><input name=password type=password><button>Login</button></form>") }
post /admin/login {
  if (request.body?.password != env_required("ADMIN_PASSWORD")) {
    return status(401, "wrong password")
  }
  return session.create({ role: "admin" }, { secret: env("SESSION_SECRET", "dev") })
}

group /admin {
  use require_admin
  get /new { return html("<form method=POST action='/admin/posts'><input name=title><br><input name=slug><br><textarea name=body_md></textarea><br><button>Publish</button></form>") }
  post /posts {
    let b = request.body
    sql.exec(db, "INSERT INTO posts (slug, title, body_md, created_at) VALUES (?, ?, ?, ?)",
      b.slug, b.title, b.body_md, now_iso())
    return redirect("/p/" + b.slug)
  }
}
`

const starterAPIApp = `// REST API — generated by mx new api
server { port: num(env("PORT", "8080")), log: true, cors: { origins: ["*"] } }

let users = [
  { id: 1, name: "Jassim", role: "admin" },
  { id: 2, name: "Ada",    role: "user"  }
]

group /api/v1 {
  get /users { return json(users) }
  get /users/:id {
    let id = num(request.params.id)
    let u = find(users, fn(x) { return x.id == id })
    if (u == null) { return status(404, { error: "not found" }) }
    return json(u)
  }
  post /users {
    let r = validate(request.body, {
      type: "object",
      required: ["name"],
      properties: { name: { type: "string", min_length: 1 } }
    })
    if (!r.valid) { return status(400, { errors: r.errors }) }
    let id = len(users) + 1
    users = push(users, { id: id, name: request.body.name, role: "user" })
    return status(201, { id: id })
  }
}

get /openapi.json { return json(openapi({ title: "Users API", version: "1.0" })) }
get /docs        { return swagger_ui("/openapi.json", { title: "Users API" }) }
get /status      { return status_page({ app: "Users API" }) }
`

// ===== mx build =====

func cmdBuild(args []string) {
	vercel := false
	wasm := false
	var file string
	for _, a := range args {
		switch a {
		case "--vercel":
			vercel = true
		case "--wasm":
			wasm = true
		default:
			if file == "" {
				file = a
			}
		}
	}
	if wasm {
		cmdBuildWasm()
		return
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
