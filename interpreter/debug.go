// debug.go — small `debug.*` namespace for assertions, tracing, and
// quick inspection. Together with `pp()` and `log.*` this rounds out
// the dev / panic-driven-development surface so MX programs can
// short-circuit cleanly when invariants break.
//
//   debug.assert(user.subscribed, "user must be subscribed here")
//   debug.unreachable("expected match arm to handle this")
//   let result = debug.trace("expensive_query", fn() { ... })
//
// All three throw on failure (so try/catch semantics work the same),
// rather than calling os.Exit — programs that want hard-stop behavior
// can wrap them in their own handler.
package interpreter

import (
	"fmt"
	"os"
	"time"
)

// debug.assert(cond, msg?) — throws when cond is falsy. Returns the
// cond unchanged on success so it composes inside expressions:
//
//   let user = debug.assert(load_user(id), "user " + id + " missing")
func builtinDebugAssert(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return Value{}, fmt.Errorf("debug.assert(cond, msg?) requires a condition")
	}
	if args[0].IsTruthy() {
		return args[0], nil
	}
	msg := "assertion failed"
	if len(args) > 1 && args[1].Kind == KindString {
		msg = "assertion failed: " + args[1].String
	}
	return Value{}, fmt.Errorf("%s", msg)
}

// debug.invariant(cond, msg?) — alias of assert with stronger naming
// for production-critical checks (the kind that should fail loudly
// in observability rather than silently corrupt state).
func builtinDebugInvariant(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return Value{}, fmt.Errorf("debug.invariant(cond, msg?)")
	}
	if args[0].IsTruthy() {
		return args[0], nil
	}
	msg := "invariant violated"
	if len(args) > 1 && args[1].Kind == KindString {
		msg = "invariant violated: " + args[1].String
	}
	return Value{}, fmt.Errorf("%s", msg)
}

// debug.unreachable(msg?) — always throws. Use to signal that a
// match arm or branch should never be hit so future readers know it
// was an intentional dead-end.
func builtinDebugUnreachable(_ *Interpreter, args []Value) (Value, error) {
	msg := "unreachable code reached"
	if len(args) > 0 && args[0].Kind == KindString {
		msg = "unreachable: " + args[0].String
	}
	return Value{}, fmt.Errorf("%s", msg)
}

// debug.trace(label, fn) — runs fn(), logs the elapsed time prefixed
// with `label`, returns whatever fn() returned. Useful as a
// throwaway profiler:
//
//   let result = debug.trace("expensive_query", fn() {
//     return sql.query(db, "...")
//   })
//   // [trace] expensive_query: 12.4ms
func builtinDebugTrace(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("debug.trace(label, fn)")
	}
	label := args[0].String
	t0 := time.Now()
	v, err := i.callFunction(nil, args[1].Function, nil)
	dur := time.Since(t0)
	fmt.Fprintf(i.Err, "[trace] %s: %s\n", label, dur)
	if err != nil {
		return Value{}, err
	}
	return v, nil
}

// debug.dump(value, label?) — alias of pp() that adds an optional
// label prefix. Convenient for stuffing into long pipelines.
func builtinDebugDump(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return NullValue(), nil
	}
	colors := isTerminal(os.Stdout)
	rendered := prettyValue(args[0], "", colors, 0)
	if len(args) > 1 && args[1].Kind == KindString {
		fmt.Printf("%s = %s\n", args[1].String, rendered)
	} else {
		fmt.Println(rendered)
	}
	return args[0], nil
}
