// body_validate.go — single-call body validation for POST/PUT handlers.
//
// The pattern handlers used to write:
//
//	post /users {
//	  let r = validate(request.body, schema)
//	  if (!r.valid) {
//	    return problem(400, "Validation failed", "", { errors: r.errors, trace_id: request.id })
//	  }
//	  let body = request.body
//	  // ... actual logic
//	}
//
// collapses to:
//
//	post /users {
//	  let r = body_validate(request, schema)
//	  if (!r.ok) { return r.response }
//	  // r.body is the validated body
//	}
//
// `r.response` is a fully-formed problem+json 400 with the right
// trace_id from request.id. Same shape as if you'd written it by hand.
package interpreter

import "fmt"

// body_validate(request, schema) -> { ok: true, body }  |  { ok: false, response }
func builtinBodyValidate(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 {
		return Value{}, fmt.Errorf("body_validate(request, schema) requires 2 arguments")
	}
	if args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("body_validate: first arg must be the request object")
	}
	if args[1].Kind != KindObject {
		return Value{}, fmt.Errorf("body_validate: schema must be an object")
	}
	body, _ := args[0].Object.Get("body")

	// Run the existing validator.
	var errs []Value
	validateValue(body, args[1], "$", &errs)
	if len(errs) == 0 {
		out := NewOrderedMap()
		out.Set("ok", BoolValue(true))
		out.Set("body", body)
		return ObjectValue(out), nil
	}

	// Build a problem+json response with errors + trace_id baked in.
	traceID := ""
	if v, ok := args[0].Object.Get("id"); ok && v.Kind == KindString {
		traceID = v.String
	}
	problemBody := NewOrderedMap()
	problemBody.Set("type", StringValue("about:blank"))
	problemBody.Set("title", StringValue("Validation failed"))
	problemBody.Set("status", NumberValue(400))
	problemBody.Set("errors", ArrayValue(errs))
	if traceID != "" {
		problemBody.Set("trace_id", StringValue(traceID))
	}
	resp := ResponseValue(&Response{
		Status:      400,
		ContentType: "application/problem+json",
		Body:        ObjectValue(problemBody),
	})

	out := NewOrderedMap()
	out.Set("ok", BoolValue(false))
	out.Set("response", resp)
	return ObjectValue(out), nil
}
