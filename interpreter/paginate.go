// paginate.go — request → page params, then page params + items → envelope.
//
// Most list endpoints reinvent the same five lines:
//
//	let page     = num(request.query?.page) || 1
//	let per_page = math.min(100, num(request.query?.per_page) || 20)
//	let offset   = (page - 1) * per_page
//	let total    = sql.first(db, "SELECT count(*) AS n FROM ...").n
//	return json({ items: ..., page, per_page, total, total_pages: ceil(...), ... })
//
// paginate() collapses the input parsing, page_response() collapses the
// envelope. Together they turn a list endpoint into:
//
//	get /users {
//	  let p = paginate(request)
//	  let total = sql.first(db, "SELECT count(*) AS n FROM users").n
//	  let items = sql.query(db, "SELECT * FROM users LIMIT ? OFFSET ?", p.limit, p.offset)
//	  return json(page_response(items, p, total))
//	}
package interpreter

import (
	"fmt"
	"math"
	"strconv"
)

// paginate(request, opts?) -> { page, per_page, limit, offset }
//
// Reads `?page=` and `?per_page=` from request.query. Defaults: page=1,
// per_page=20. opts: { default_per_page, max_per_page } override.
//
// `limit` and `offset` are SQL-ready (`LIMIT ? OFFSET ?`). `page` and
// `per_page` are echoed back to the caller for the envelope.
func builtinPaginate(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("paginate(request, opts?) requires the request object")
	}
	defaultPerPage := 20
	maxPerPage := 100
	if len(args) > 1 && args[1].Kind == KindObject {
		o := args[1].Object
		if v, ok := o.Get("default_per_page"); ok && v.Kind == KindNumber {
			defaultPerPage = int(v.Number)
		}
		if v, ok := o.Get("max_per_page"); ok && v.Kind == KindNumber {
			maxPerPage = int(v.Number)
		}
	}

	page := 1
	perPage := defaultPerPage
	if q, ok := args[0].Object.Get("query"); ok && q.Kind == KindObject {
		if v, ok := q.Object.Get("page"); ok {
			if n := coerceInt(v); n > 0 {
				page = n
			}
		}
		if v, ok := q.Object.Get("per_page"); ok {
			if n := coerceInt(v); n > 0 {
				perPage = n
			}
		}
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	if perPage < 1 {
		perPage = 1
	}

	out := NewOrderedMap()
	out.Set("page", NumberValue(float64(page)))
	out.Set("per_page", NumberValue(float64(perPage)))
	out.Set("limit", NumberValue(float64(perPage)))
	out.Set("offset", NumberValue(float64((page-1)*perPage)))
	return ObjectValue(out), nil
}

// page_response(items, page_info, total) -> envelope object
//
// Builds the conventional list-endpoint shape:
//
//	{
//	  items, page, per_page, total,
//	  total_pages, has_next, has_prev
//	}
//
// Pass the value `paginate()` returned as `page_info`.
func builtinPageResponse(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 3 {
		return Value{}, fmt.Errorf("page_response(items, page_info, total) requires 3 args")
	}
	if args[0].Kind != KindArray {
		return Value{}, fmt.Errorf("page_response: items must be an array")
	}
	if args[1].Kind != KindObject {
		return Value{}, fmt.Errorf("page_response: page_info must be the object returned by paginate()")
	}
	if args[2].Kind != KindNumber {
		return Value{}, fmt.Errorf("page_response: total must be a number")
	}
	pageV, _ := args[1].Object.Get("page")
	perPageV, _ := args[1].Object.Get("per_page")
	page := int(pageV.Number)
	perPage := int(perPageV.Number)
	total := int(args[2].Number)

	totalPages := 0
	if perPage > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(perPage)))
	}

	out := NewOrderedMap()
	out.Set("items", args[0])
	out.Set("page", NumberValue(float64(page)))
	out.Set("per_page", NumberValue(float64(perPage)))
	out.Set("total", NumberValue(float64(total)))
	out.Set("total_pages", NumberValue(float64(totalPages)))
	out.Set("has_next", BoolValue(page < totalPages))
	out.Set("has_prev", BoolValue(page > 1))
	return ObjectValue(out), nil
}

// coerceInt parses a Value into an int. Numbers truncate; strings
// parse via strconv. Returns 0 for any other shape so callers can
// distinguish "missing" from "given an invalid value" (both → default).
func coerceInt(v Value) int {
	switch v.Kind {
	case KindNumber:
		return int(v.Number)
	case KindString:
		n, err := strconv.Atoi(v.String)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}
