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
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jlkdevelop/mxscript/interpreter"
	"github.com/jlkdevelop/mxscript/lexer"
	"github.com/jlkdevelop/mxscript/parser"
)

// Version is bumped at release time. Override at build with:
//
//	go build -ldflags "-X main.Version=v0.2.0"
var Version = "v0.1.0"

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

	if file == "" {
		fatal("usage: mx run <file.mx>")
	}

	if watch {
		runWatched(file, port, debug)
		return
	}

	if err := runOnce(file, port, debug); err != nil {
		fmt.Fprintf(os.Stderr, "%serror:%s %s\n", cRed, cReset, err)
		os.Exit(1)
	}
}

func runOnce(file string, port int, debug bool) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", file, err)
	}

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

// ===== mx build =====

func cmdBuild(args []string) {
	if len(args) == 0 {
		fatal("usage: mx build <file.mx>")
	}
	file := args[0]
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("cannot read %s: %v", file, err)
	}
	tokens, err := lexer.New(string(src)).Tokenize()
	if err != nil {
		fatal("%s: %v", file, err)
	}
	if _, err := parser.New(tokens).Parse(); err != nil {
		fatal("%s: %v", file, err)
	}
	fmt.Printf("%s✓%s %s parses cleanly\n", cGreen, cReset, file)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%serror:%s "+format+"\n", append([]any{cRed, cReset}, args...)...)
	os.Exit(1)
}
