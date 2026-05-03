// problem.go — RFC 7807 application/problem+json responses.
//
// The standard machine-readable error shape:
//
//   { "type": "...", "title": "...", "status": 400, "detail": "...", ... }
//
// Pattern in handlers:
//
//   post /users {
//     let r = validate(request.body, schema)
//     if (!r.valid) {
//       return problem(400, "Validation failed", { errors: r.errors })
//     }
//   }
//
//   get /users/:id {
//     let u = sql.first(db, "SELECT * FROM users WHERE id = ?", request.params.id)
//     if (u == null) { return problem(404, "User not found") }
//     return json(u)
//   }
//
// Anything beyond status/title/detail goes into the `ext` object and
// is merged into the top-level response — RFC 7807 §3.2 explicitly
// allows arbitrary extension members.
package interpreter

import "fmt"

// problem(status, title, detail?, ext?) -> Response
//
// `detail` is a string (human-readable).
// `ext` is an object whose keys are spread onto the top-level body
// (e.g. `{ errors: [...], trace_id: "..." }`).
//
// Content-Type is set to application/problem+json per RFC 7807 §3.
func builtinProblem(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindNumber || args[1].Kind != KindString {
		return Value{}, fmt.Errorf("problem(status, title, detail?, ext?) requires (number, string, ...)")
	}
	status := int(args[0].Number)
	title := args[1].String

	body := NewOrderedMap()
	body.Set("type", StringValue("about:blank"))
	body.Set("title", StringValue(title))
	body.Set("status", NumberValue(float64(status)))

	if len(args) > 2 && args[2].Kind == KindString && args[2].String != "" {
		body.Set("detail", StringValue(args[2].String))
	}

	// Extension members — spread the ext object onto the top-level body.
	// Last writer wins, so callers can override `type` for a richer
	// problem-type URI.
	if len(args) > 3 && args[3].Kind == KindObject {
		for _, k := range args[3].Object.Keys {
			v, _ := args[3].Object.Get(k)
			body.Set(k, v)
		}
	}

	return ResponseValue(&Response{
		Status:      status,
		ContentType: "application/problem+json",
		Body:        ObjectValue(body),
	}), nil
}
