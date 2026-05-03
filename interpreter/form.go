// form.go — application/x-www-form-urlencoded helpers. The HTTP
// router auto-decodes form bodies into request.body for incoming
// requests, so mostly these are for outbound calls + tests.
//
//	form.parse("a=1&b=2&c=hello%20world")
//	// -> { a: "1", b: "2", c: "hello world" }
//
//	form.encode({ user: "alice", count: 3 })
//	// -> "count=3&user=alice"
//
//	// Building a POST body:
//	fetch(url, { method: "POST", body: form.encode({ ... }) })
package interpreter

import (
	"fmt"
	neturl "net/url"
	"sort"
)

// form.parse(s) — turns a urlencoded query string into an object.
// Multi-valued keys collapse to the last value (matches the common
// HTTP-form convention; see net/url ParseQuery for the underlying
// behavior). Returns null on malformed input — easier to compose
// with `if (parsed == null) ...` than try/catch.
func builtinFormParse(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("form.parse(s) requires a string")
	}
	values, err := neturl.ParseQuery(args[0].String)
	if err != nil {
		return NullValue(), nil
	}
	out := NewOrderedMap()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vs := values[k]
		if len(vs) == 0 {
			continue
		}
		// Single-valued: keep as a string. Multi-valued: keep as an
		// array — matches what users actually want for things like
		// `tags=a&tags=b&tags=c`.
		if len(vs) == 1 {
			out.Set(k, StringValue(vs[0]))
		} else {
			arr := make([]Value, len(vs))
			for i, v := range vs {
				arr[i] = StringValue(v)
			}
			out.Set(k, ArrayValue(arr))
		}
	}
	return ObjectValue(out), nil
}

// form.encode(obj) — turns an object into a urlencoded string with
// keys sorted alphabetically (deterministic, makes signing /
// caching easier). Number / bool values stringify; arrays expand
// into repeated keys (`tags=a&tags=b`).
func builtinFormEncode(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("form.encode(obj) requires an object")
	}
	values := neturl.Values{}
	for _, k := range args[0].Object.Keys {
		v, _ := args[0].Object.Get(k)
		switch v.Kind {
		case KindArray:
			for _, el := range v.Array {
				values.Add(k, el.Display())
			}
		case KindNull:
			// Skip null entries — matches what `let body = {x: maybe}`
			// patterns expect (don't send a literal "null" string).
		default:
			values.Set(k, v.Display())
		}
	}
	return StringValue(values.Encode()), nil
}
