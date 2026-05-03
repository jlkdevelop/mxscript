//go:build js && wasm

// mxwasm is the WebAssembly entry point for MX Script. It runs in
// the browser, exposes a single JS function `mxRun(source) -> result`,
// and wraps the same lex/parse/eval pipeline used by the native CLI.
//
// Two interesting differences from native:
//
//  1. SQL / Redis / Jobs / SMTP are stubbed (see interpreter/sql_wasm.go
//     etc.) — the playground intentionally does not ship a database.
//  2. stdout/stderr are captured into a string buffer and returned
//     to JS instead of being written to a console — the page renders
//     the result however it wants.
package main

import (
	"bytes"
	"fmt"
	"syscall/js"

	"github.com/jlkdevelop/mxscript/interpreter"
	"github.com/jlkdevelop/mxscript/lexer"
	"github.com/jlkdevelop/mxscript/parser"
)

// runMX is the function exposed to JavaScript. It accepts one string
// argument (the MX source) and returns an object:
//
//	{ stdout: string, stderr: string, error: string|null }
//
// `error` is non-null when lex/parse fails or the program throws at
// runtime; `stdout` always carries whatever the program managed to
// emit before the failure, so playgrounds can render partial output.
func runMX(this js.Value, args []js.Value) any {
	if len(args) < 1 || args[0].Type() != js.TypeString {
		return resultObject("", "", "runMX(source) requires a string argument")
	}
	src := args[0].String()

	tokens, err := lexer.New(src).Tokenize()
	if err != nil {
		return resultObject("", "", fmt.Sprintf("lex: %v", err))
	}
	prog, err := parser.New(tokens).Parse()
	if err != nil {
		return resultObject("", "", fmt.Sprintf("parse: %v", err))
	}

	var stdout, stderr bytes.Buffer
	interp := interpreter.New()
	interp.SetFile("<playground>")
	interp.Out = &stdout
	interp.Err = &stderr

	// Use Exec instead of Run so we don't try to start an HTTP server
	// (impossible in the browser anyway). Programs that declare routes
	// will simply have those declarations registered and ignored.
	if _, err := interp.Exec(prog); err != nil {
		return resultObject(stdout.String(), stderr.String(), err.Error())
	}
	return resultObject(stdout.String(), stderr.String(), "")
}

// resultObject builds the JS-side return shape. We construct a JS
// Object via Global().Get("Object").New() so the JS host code can
// destructure it normally (`const { stdout, stderr, error } = ...`).
func resultObject(stdout, stderr, errMsg string) js.Value {
	obj := js.Global().Get("Object").New()
	obj.Set("stdout", js.ValueOf(stdout))
	obj.Set("stderr", js.ValueOf(stderr))
	if errMsg == "" {
		obj.Set("error", js.Null())
	} else {
		obj.Set("error", js.ValueOf(errMsg))
	}
	return obj
}

func main() {
	js.Global().Set("mxRun", js.FuncOf(runMX))
	// Block forever so the goroutine doesn't exit and the JS host
	// keeps a live reference to the exported function.
	<-make(chan struct{})
}
