// shell_helpers.go — convenience wrappers around the existing
// `shell()` builtin. Three patterns:
//
//	shell.run(cmd, args?, opts?)    -> exit_code  (just the exit code)
//	shell.output(cmd, args?, opts?) -> string     (stdout, fail on non-zero)
//	shell.bash(script, opts?)       -> result     (run a multi-line script)
//	shell.which(name)               -> string|null  (locate on PATH)
//
// All four delegate to the existing builtinShell so they share env /
// dir / timeout / stdin opts.
package interpreter

import (
	"fmt"
	"os/exec"
)

// shell.run — fire-and-forget. Returns the exit code as a number;
// throws on plumbing errors (command not found, timeout) but NOT on
// non-zero exit. Pair with `if (shell.run(...) != 0) ...` to handle
// failures explicitly.
func builtinShellRun(i *Interpreter, args []Value) (Value, error) {
	v, err := builtinShell(i, args)
	if err != nil {
		return Value{}, err
	}
	code, _ := v.Object.Get("exit_code")
	return code, nil
}

// shell.output — returns stdout as a string. Throws on non-zero exit
// so callers can rely on the result being valid. Stderr is captured
// in the error message for debugging.
func builtinShellOutput(i *Interpreter, args []Value) (Value, error) {
	v, err := builtinShell(i, args)
	if err != nil {
		return Value{}, err
	}
	exit, _ := v.Object.Get("exit_code")
	if exit.Number != 0 {
		stderr, _ := v.Object.Get("stderr")
		return Value{}, fmt.Errorf("shell.output: exited %d: %s", int(exit.Number), stderr.String)
	}
	stdout, _ := v.Object.Get("stdout")
	return stdout, nil
}

// shell.bash — run a multi-line bash script via `bash -c`. Useful
// for pipelines and conditional logic that don't fit one command.
//
//	shell.bash("ls *.mx | wc -l | tr -d ' '")
func builtinShellBash(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("shell.bash(script, opts?) requires a script string")
	}
	// Re-shape the args into shell(cmd, [args], opts) form.
	bashArgs := []Value{
		StringValue("bash"),
		ArrayValue([]Value{StringValue("-c"), StringValue(args[0].String)}),
	}
	if len(args) > 1 {
		bashArgs = append(bashArgs, args[1])
	}
	return builtinShell(i, bashArgs)
}

// shell.which — locate an executable on $PATH. Returns the absolute
// path or null when missing. Useful for graceful capability checks
// (`if (shell.which("ffmpeg") == null) { return error("ffmpeg required") }`).
func builtinShellWhich(_ *Interpreter, args []Value) (Value, error) {
	name, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return NullValue(), nil
	}
	return StringValue(path), nil
}
