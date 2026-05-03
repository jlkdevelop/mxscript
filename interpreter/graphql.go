// graphql.go — minimal GraphQL handler. Parses a useful subset of the
// query language (queries, mutations, fields with arguments, nested
// selection sets, variables, aliases) and dispatches to a user-
// supplied resolver tree. Skips: fragments, interfaces, unions,
// directives, introspection, subscriptions. Apollo / urql / Relay
// clients all work for the common case where you write a backend
// from scratch.
//
// The schema is implicit in the resolver shape:
//
//	graphql.handler({
//	  Query: {
//	    user: fn(args, ctx) {
//	      return sql.query_one(db, "SELECT * FROM users WHERE id = ?", args.id)
//	    },
//	    users: fn(args, ctx) {
//	      return sql.query(db, "SELECT * FROM users LIMIT ?", args.limit ?? 50)
//	    },
//	  },
//	  Mutation: {
//	    create_user: fn(args, ctx) { ... }
//	  },
//	  User: {
//	    posts: fn(parent, args, ctx) {
//	      return sql.query(db, "SELECT * FROM posts WHERE user_id = ?", parent.id)
//	    }
//	  }
//	})
//
// Routes mount the handler with `route POST /graphql { return graphql.handler(...)(request) }`.
package interpreter

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// gqlField represents one field in a selection set.
type gqlField struct {
	Alias    string         // empty unless `aliased: foo`
	Name     string         // field name
	Args     map[string]any // argument literals + variables resolved
	Children []gqlField     // nested selection set
}

type gqlOperation struct {
	Type   string // "query" | "mutation"
	Name   string
	Fields []gqlField
}

// parseGraphQL is a hand-rolled lexer + recursive-descent parser. We
// keep it small (~150 lines) by leaning on the JSON parser for nested
// argument literals — anything past a simple identifier / number /
// string falls through to json.Unmarshal.
type gqlParser struct {
	src string
	pos int
}

func (p *gqlParser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *gqlParser) skip() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' {
			p.pos++
			continue
		}
		if c == '#' { // line comment
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		break
	}
}

func (p *gqlParser) ident() string {
	p.skip()
	start := p.pos
	for p.pos < len(p.src) {
		c := rune(p.src[p.pos])
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' {
			p.pos++
			continue
		}
		break
	}
	return p.src[start:p.pos]
}

func (p *gqlParser) parseOperation() (gqlOperation, error) {
	op := gqlOperation{Type: "query"}
	p.skip()
	// Optional operation type / name.
	if p.peek() != '{' {
		t := p.ident()
		if t == "query" || t == "mutation" {
			op.Type = t
			p.skip()
			if p.peek() != '{' && p.peek() != '(' {
				op.Name = p.ident()
			}
		} else {
			// Anonymous selection set — let `t` be the implicit op name.
			op.Name = t
		}
	}
	p.skip()
	// Optional variable-definitions list: `query Q($uid: Int!) { ... }`.
	// We don't enforce the type system, so skipping the (...) block is
	// enough — actual values come in via the variables map.
	if p.peek() == '(' {
		depth := 0
		for p.pos < len(p.src) {
			c := p.src[p.pos]
			p.pos++
			if c == '(' {
				depth++
			} else if c == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		p.skip()
	}
	if p.peek() != '{' {
		return op, fmt.Errorf("graphql: expected '{', got %q", p.snippet())
	}
	fields, err := p.parseSelectionSet()
	if err != nil {
		return op, err
	}
	op.Fields = fields
	return op, nil
}

func (p *gqlParser) parseSelectionSet() ([]gqlField, error) {
	p.skip()
	if p.peek() != '{' {
		return nil, fmt.Errorf("graphql: expected '{' at %s", p.snippet())
	}
	p.pos++ // consume {
	var out []gqlField
	for {
		p.skip()
		if p.peek() == '}' {
			p.pos++
			return out, nil
		}
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("graphql: unterminated selection set")
		}
		f, err := p.parseField()
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
}

func (p *gqlParser) parseField() (gqlField, error) {
	p.skip()
	first := p.ident()
	if first == "" {
		return gqlField{}, fmt.Errorf("graphql: expected field name at %s", p.snippet())
	}
	f := gqlField{Name: first}
	p.skip()
	// Alias: `name: realName`
	if p.peek() == ':' {
		p.pos++
		p.skip()
		realName := p.ident()
		if realName == "" {
			return f, fmt.Errorf("graphql: expected name after alias colon")
		}
		f.Alias = first
		f.Name = realName
	}
	p.skip()
	// Arguments
	if p.peek() == '(' {
		p.pos++
		args, err := p.parseArgs()
		if err != nil {
			return f, err
		}
		f.Args = args
	}
	p.skip()
	// Selection set
	if p.peek() == '{' {
		children, err := p.parseSelectionSet()
		if err != nil {
			return f, err
		}
		f.Children = children
	}
	return f, nil
}

func (p *gqlParser) parseArgs() (map[string]any, error) {
	out := map[string]any{}
	for {
		p.skip()
		if p.peek() == ')' {
			p.pos++
			return out, nil
		}
		name := p.ident()
		if name == "" {
			return out, fmt.Errorf("graphql: expected argument name at %s", p.snippet())
		}
		p.skip()
		if p.peek() != ':' {
			return out, fmt.Errorf("graphql: expected ':' after %q", name)
		}
		p.pos++
		val, err := p.parseValue()
		if err != nil {
			return out, err
		}
		out[name] = val
	}
}

func (p *gqlParser) parseValue() (any, error) {
	p.skip()
	c := p.peek()
	switch {
	case c == '"':
		end := strings.Index(p.src[p.pos+1:], `"`)
		if end < 0 {
			return nil, fmt.Errorf("graphql: unterminated string")
		}
		s := p.src[p.pos+1 : p.pos+1+end]
		p.pos += end + 2
		return s, nil
	case c == '[':
		// Use json.Unmarshal on the bracketed literal for arrays.
		return p.parseJSONLiteral('[', ']')
	case c == '{':
		return p.parseJSONLiteral('{', '}')
	case c == '-' || (c >= '0' && c <= '9'):
		start := p.pos
		for p.pos < len(p.src) {
			c := p.src[p.pos]
			if c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9') || c == 'e' || c == 'E' {
				p.pos++
				continue
			}
			break
		}
		var n float64
		if err := json.Unmarshal([]byte(p.src[start:p.pos]), &n); err != nil {
			return nil, err
		}
		return n, nil
	case c == '$':
		// Variable reference — left as a `$name` string for the caller
		// to bind from the variables map.
		p.pos++
		name := p.ident()
		return "$" + name, nil
	default:
		// true / false / null / enum.
		ident := p.ident()
		switch ident {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		default:
			return ident, nil // enum-like; pass through as string
		}
	}
}

// parseJSONLiteral collects characters until the matching close bracket,
// respecting strings + nested brackets, and feeds the slab to
// json.Unmarshal. It's not strictly GraphQL syntax (which uses single-
// line argument literals) but it covers the cases users actually pass.
func (p *gqlParser) parseJSONLiteral(open, close byte) (any, error) {
	depth := 0
	start := p.pos
	inStr := false
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if inStr {
			if c == '"' && p.src[p.pos-1] != '\\' {
				inStr = false
			}
			p.pos++
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				p.pos++
				slab := p.src[start:p.pos]
				var v any
				if err := json.Unmarshal([]byte(slab), &v); err != nil {
					return nil, fmt.Errorf("graphql: invalid literal %q: %w", slab, err)
				}
				return v, nil
			}
		}
		p.pos++
	}
	return nil, fmt.Errorf("graphql: unterminated literal")
}

func (p *gqlParser) snippet() string {
	end := p.pos + 16
	if end > len(p.src) {
		end = len(p.src)
	}
	return p.src[p.pos:end]
}

// ===== Execution =====

// gqlExecute walks the parsed operation, looking up resolvers per
// field on the resolver tree. The resolver tree is the OrderedMap MX
// passed in: top-level keys are type names ("Query", "Mutation",
// "User"), each holding a map of field-resolver functions.
func gqlExecute(i *Interpreter, op gqlOperation, resolvers *OrderedMap, variables map[string]any) (Value, error) {
	rootName := "Query"
	if op.Type == "mutation" {
		rootName = "Mutation"
	}
	root, ok := resolvers.Get(rootName)
	if !ok || root.Kind != KindObject {
		return Value{}, fmt.Errorf("graphql: resolver tree missing %q", rootName)
	}
	out := NewOrderedMap()
	for _, f := range op.Fields {
		result, err := gqlResolveField(i, f, root.Object, NullValue(), resolvers, variables)
		if err != nil {
			return Value{}, err
		}
		key := f.Name
		if f.Alias != "" {
			key = f.Alias
		}
		out.Set(key, result)
	}
	return ObjectValue(out), nil
}

// gqlResolveField calls the resolver for one field, then recurses
// into the selection set if the result is an object/array of objects
// and the user asked for sub-fields.
func gqlResolveField(i *Interpreter, f gqlField, resolverNode *OrderedMap, parent Value, resolvers *OrderedMap, variables map[string]any) (Value, error) {
	resolver, ok := resolverNode.Get(f.Name)
	if !ok || resolver.Kind != KindFunction {
		// No resolver: if `parent` is an object, just read the field.
		if parent.Kind == KindObject {
			if v, ok := parent.Object.Get(f.Name); ok {
				return v, nil
			}
		}
		return NullValue(), nil
	}
	args := gqlGoToValue(resolveVariables(f.Args, variables))
	resolverArgs := []Value{args}
	if parent.Kind != KindNull {
		resolverArgs = []Value{parent, args}
	}
	v, err := i.callFunction(nil, resolver.Function, resolverArgs)
	if err != nil {
		return Value{}, err
	}
	// If the user asked for sub-fields, recurse.
	if len(f.Children) > 0 {
		return gqlSelectFields(i, v, f.Children, resolvers, variables)
	}
	return v, nil
}

// gqlSelectFields walks the children selection set against the
// resolved value. For objects, picks the named fields; for arrays of
// objects, maps over each element.
func gqlSelectFields(i *Interpreter, v Value, children []gqlField, resolvers *OrderedMap, variables map[string]any) (Value, error) {
	switch v.Kind {
	case KindObject:
		// Find a typed resolver bag if one exists for this object.
		// Heuristic: if there's a `__typename` field, use that; otherwise
		// inline-resolve via the parent object's keys.
		typeName := ""
		if t, ok := v.Object.Get("__typename"); ok && t.Kind == KindString {
			typeName = t.String
		}
		typedResolvers := NewOrderedMap()
		if typeName != "" {
			if tr, ok := resolvers.Get(typeName); ok && tr.Kind == KindObject {
				typedResolvers = tr.Object
			}
		}
		out := NewOrderedMap()
		for _, child := range children {
			result, err := gqlResolveField(i, child, typedResolvers, v, resolvers, variables)
			if err != nil {
				return Value{}, err
			}
			key := child.Name
			if child.Alias != "" {
				key = child.Alias
			}
			out.Set(key, result)
		}
		return ObjectValue(out), nil
	case KindArray:
		out := make([]Value, 0, len(v.Array))
		for _, el := range v.Array {
			r, err := gqlSelectFields(i, el, children, resolvers, variables)
			if err != nil {
				return Value{}, err
			}
			out = append(out, r)
		}
		return ArrayValue(out), nil
	}
	return v, nil
}

// resolveVariables walks the args map and replaces any "$name" string
// values with the matching entry from the variables map. Lets clients
// send `query Q($id: Int!) { user(id: $id) { name } }` with a
// `variables` payload — the standard Apollo flow.
func resolveVariables(args map[string]any, variables map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && strings.HasPrefix(s, "$") {
			if bound, ok := variables[s[1:]]; ok {
				out[k] = bound
				continue
			}
		}
		out[k] = v
	}
	return out
}

// gqlGoToValue converts a Go value (from the parsed args) into MX Value.
// The reverse is goArgs / valueToGo elsewhere; this is just for query
// argument literals and resolved variables.
func gqlGoToValue(v any) Value {
	switch x := v.(type) {
	case nil:
		return NullValue()
	case bool:
		return BoolValue(x)
	case float64:
		return NumberValue(x)
	case string:
		return StringValue(x)
	case []any:
		out := make([]Value, len(x))
		for i, el := range x {
			out[i] = gqlGoToValue(el)
		}
		return ArrayValue(out)
	case map[string]any:
		om := NewOrderedMap()
		for k, val := range x {
			om.Set(k, gqlGoToValue(val))
		}
		return ObjectValue(om)
	case map[string]string:
		om := NewOrderedMap()
		for k, val := range x {
			om.Set(k, StringValue(val))
		}
		return ObjectValue(om)
	}
	return NullValue()
}

// ===== Builtin =====

// graphql.handler(resolvers) — returns a function suitable for direct
// return from a `route POST /graphql` handler. The function reads
// `request.body.query` and `request.body.variables`, parses the
// query, and dispatches to the resolvers.
func builtinGraphQLHandler(i *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("graphql.handler(resolvers) requires the resolvers object")
	}
	resolvers := args[0].Object
	handler := &Function{Name: "graphql.dispatch", Native: func(interp *Interpreter, callArgs []Value) (Value, error) {
		if len(callArgs) < 1 {
			return Value{}, fmt.Errorf("graphql handler needs the parsed request body")
		}
		body := callArgs[0]
		query := ""
		variables := map[string]any{}
		if body.Kind == KindObject {
			if v, ok := body.Object.Get("query"); ok && v.Kind == KindString {
				query = v.String
			}
			if v, ok := body.Object.Get("variables"); ok && v.Kind == KindObject {
				for _, k := range v.Object.Keys {
					val, _ := v.Object.Get(k)
					variables[k] = goNative(val)
				}
			}
		} else if body.Kind == KindString {
			query = body.String
		}
		p := &gqlParser{src: query}
		op, err := p.parseOperation()
		if err != nil {
			return gqlError(err), nil
		}
		data, err := gqlExecute(interp, op, resolvers, variables)
		if err != nil {
			return gqlError(err), nil
		}
		out := NewOrderedMap()
		out.Set("data", data)
		return ObjectValue(out), nil
	}}
	return FunctionValue(handler), nil
}

func gqlError(err error) Value {
	out := NewOrderedMap()
	errs := NewOrderedMap()
	errs.Set("message", StringValue(err.Error()))
	out.Set("errors", ArrayValue([]Value{ObjectValue(errs)}))
	return ObjectValue(out)
}

// goNative is a Value -> any converter for the variables map. We
// mirror the behavior of the existing valueToGo / valueToPlainGo
// helpers but only need the JSON-shapes GraphQL variables actually
// carry.
func goNative(v Value) any {
	switch v.Kind {
	case KindNull:
		return nil
	case KindBool:
		return v.Bool
	case KindNumber:
		return v.Number
	case KindString:
		return v.String
	case KindArray:
		out := make([]any, len(v.Array))
		for i, el := range v.Array {
			out[i] = goNative(el)
		}
		return out
	case KindObject:
		out := map[string]any{}
		for _, k := range v.Object.Keys {
			val, _ := v.Object.Get(k)
			out[k] = goNative(val)
		}
		return out
	}
	return nil
}
