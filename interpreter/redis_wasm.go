//go:build js

// redis_wasm.go — js/wasm stubs for redis builtins. The browser can't
// hold raw TCP connections, so the actual go-redis driver is gated
// behind `!js`. Calling any of these in a wasm build returns a clear
// error so the program can fail fast and the user can see why.
package interpreter

import "fmt"

type redisHandle struct{}

var errRedisUnsupported = fmt.Errorf("redis is unsupported on the wasm build (no raw TCP from the browser)")

func builtinRedisConnect(_ *Interpreter, _ []Value) (Value, error) {
	return Value{}, errRedisUnsupported
}
func builtinRedisSet(_ *Interpreter, _ []Value) (Value, error)  { return Value{}, errRedisUnsupported }
func builtinRedisGet(_ *Interpreter, _ []Value) (Value, error)  { return Value{}, errRedisUnsupported }
func builtinRedisDel(_ *Interpreter, _ []Value) (Value, error)  { return Value{}, errRedisUnsupported }
func builtinRedisIncr(_ *Interpreter, _ []Value) (Value, error) { return Value{}, errRedisUnsupported }
func builtinRedisExpire(_ *Interpreter, _ []Value) (Value, error) {
	return Value{}, errRedisUnsupported
}
func builtinRedisPublish(_ *Interpreter, _ []Value) (Value, error) {
	return Value{}, errRedisUnsupported
}
func builtinRedisClose(_ *Interpreter, _ []Value) (Value, error) { return Value{}, errRedisUnsupported }
