//go:build js

// search_wasm.go — js/wasm stubs for the FTS5 search namespace.
// FTS5 ships with the SQLite driver which is itself gated to !js,
// so we mirror that with clear-error stubs.
package interpreter

func builtinSearchCreate(_ *Interpreter, _ []Value) (Value, error) { return Value{}, errSQLUnsupported }
func builtinSearchIndex(_ *Interpreter, _ []Value) (Value, error)  { return Value{}, errSQLUnsupported }
func builtinSearchQuery(_ *Interpreter, _ []Value) (Value, error)  { return Value{}, errSQLUnsupported }
func builtinSearchDelete(_ *Interpreter, _ []Value) (Value, error) { return Value{}, errSQLUnsupported }

// quoteIdent and toGoArg are also referenced from non-test code paths
// — provide stub-only implementations under js so the package links.
func quoteIdent(name string) string { return name }
func toGoArg(v Value) any           { return nil }
