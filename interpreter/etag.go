// etag.go — HTTP caching primitives for read-heavy endpoints.
//
// The pattern:
//
//   get /users/:id {
//     let user = sql.first(db, "SELECT * FROM users WHERE id = ?", request.params.id)
//     let tag  = etag(user)
//     if (request.headers["if-none-match"] == tag) { return not_modified() }
//     return json(user, { headers: { "ETag": tag, "Cache-Control": "private, max-age=60" } })
//   }
//
// One round-trip cost on the cold path; zero body bytes on the warm path.
// Big win for list/detail endpoints that serve the same shape to the same
// client over and over.
package interpreter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
)

// etag(value) -> string — stable strong-hash etag for any value.
//
// We render the value via jsonEncode (deterministic key order via
// OrderedMap) and SHA-256 the bytes, returning the first 16 hex
// chars wrapped in double quotes per RFC 7232 §2.3 syntax. Strings
// hash their own bytes directly — handy for hashing already-rendered
// HTML or text bodies without re-encoding.
func builtinEtag(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 {
		return Value{}, fmt.Errorf("etag(value) requires 1 argument")
	}
	v := args[0]
	var raw []byte
	if v.Kind == KindString {
		raw = []byte(v.String)
	} else {
		b, err := jsonEncode(v)
		if err != nil {
			return Value{}, fmt.Errorf("etag: cannot encode value: %w", err)
		}
		raw = b
	}
	sum := sha256.Sum256(raw)
	return StringValue(`"` + hex.EncodeToString(sum[:8]) + `"`), nil
}

// not_modified() -> response — 304 Not Modified, no body. Pair with
// etag() and an If-None-Match check.
func builtinNotModified(_ *Interpreter, _ []Value) (Value, error) {
	return ResponseValue(&Response{
		Status:      http.StatusNotModified,
		ContentType: "",
		Body:        StringValue(""),
	}), nil
}

// cache_control(opts) -> string — build a Cache-Control header value
// from a directive object. Skipping the string-glue ceremony: pass
// `{ public: true, max_age: 300, immutable: true }` and get back
// `"public, max-age=300, immutable"`. Order is deterministic so
// snapshot tests don't flap.
//
// Recognised keys (all optional):
//   public, private, no_cache, no_store, must_revalidate, immutable  (bool)
//   max_age, s_max_age, stale_while_revalidate, stale_if_error      (number, seconds)
func builtinCacheControl(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("cache_control(opts) requires an options object")
	}
	o := args[0].Object
	var parts []string
	addBool := func(key, directive string) {
		if v, ok := o.Get(key); ok && v.Kind == KindBool && v.Bool {
			parts = append(parts, directive)
		}
	}
	addNum := func(key, directive string) {
		if v, ok := o.Get(key); ok && v.Kind == KindNumber {
			parts = append(parts, fmt.Sprintf("%s=%d", directive, int64(v.Number)))
		}
	}
	// Conventional order: visibility, then revalidation, then durations,
	// then `immutable` last. Matches what CDN docs and Cache-Control
	// header examples in the wild look like.
	addBool("public", "public")
	addBool("private", "private")
	addBool("no_cache", "no-cache")
	addBool("no_store", "no-store")
	addBool("must_revalidate", "must-revalidate")
	addNum("max_age", "max-age")
	addNum("s_max_age", "s-maxage")
	addNum("stale_while_revalidate", "stale-while-revalidate")
	addNum("stale_if_error", "stale-if-error")
	addBool("immutable", "immutable")
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return StringValue(out), nil
}
