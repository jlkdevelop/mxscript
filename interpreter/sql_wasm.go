//go:build js

// sql_wasm.go — js/wasm stubs for the SQL helpers. The sqlite driver
// (modernc.org/sqlite) depends on libc shims that don't compile for
// the browser, so the production sql.go is gated behind `!js`. This
// file provides the same exported symbols with runtime errors so the
// rest of the interpreter still builds and links cleanly.
//
// Browser-side users who genuinely need SQL should call out to a
// remote API; the playground intentionally doesn't ship a database.
package interpreter

import "fmt"

type dbHandle struct{}

type sqlRunner interface{}

func (h *dbHandle) runner() sqlRunner { return nil }

var errSQLUnsupported = fmt.Errorf("sql is unsupported on the wasm build (no sqlite driver in the browser)")

func sqlOpen(path string) (*dbHandle, error)                      { return nil, errSQLUnsupported }
func sqlExec(h *dbHandle, q string, args []Value) (Value, error)  { return Value{}, errSQLUnsupported }
func sqlQuery(h *dbHandle, q string, args []Value) (Value, error) { return Value{}, errSQLUnsupported }
func sqlValueToMX(v any) Value                                    { return NullValue() }
func goArgs(args []Value) []any                                   { return nil }

// Builtin shims — return the same "unsupported" error so route
// handlers can detect the platform mismatch and degrade gracefully.
func builtinSQLOpen(i *Interpreter, args []Value) (Value, error)        { return Value{}, errSQLUnsupported }
func mustDBHandle(args []Value) (*dbHandle, error)                      { return nil, errSQLUnsupported }
func builtinSQLExec(i *Interpreter, args []Value) (Value, error)        { return Value{}, errSQLUnsupported }
func builtinSQLQuery(i *Interpreter, args []Value) (Value, error)       { return Value{}, errSQLUnsupported }
func builtinSQLQueryOne(i *Interpreter, args []Value) (Value, error)    { return Value{}, errSQLUnsupported }
func builtinSQLClose(i *Interpreter, args []Value) (Value, error)       { return Value{}, errSQLUnsupported }
func builtinSQLMigrate(i *Interpreter, args []Value) (Value, error)     { return Value{}, errSQLUnsupported }
func builtinSQLTransaction(i *Interpreter, args []Value) (Value, error) { return Value{}, errSQLUnsupported }
func builtinSQLInsert(i *Interpreter, args []Value) (Value, error)      { return Value{}, errSQLUnsupported }
func builtinSQLUpsert(i *Interpreter, args []Value) (Value, error)      { return Value{}, errSQLUnsupported }
func builtinSQLUpdate(i *Interpreter, args []Value) (Value, error)      { return Value{}, errSQLUnsupported }
func builtinSQLDelete(i *Interpreter, args []Value) (Value, error)      { return Value{}, errSQLUnsupported }
func builtinSQLFind(i *Interpreter, args []Value) (Value, error)        { return Value{}, errSQLUnsupported }
func builtinSQLFindOne(i *Interpreter, args []Value) (Value, error)     { return Value{}, errSQLUnsupported }
