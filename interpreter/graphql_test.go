package interpreter

import (
	"strings"
	"testing"
)

func TestGraphQLParseSimpleQuery(t *testing.T) {
	p := &gqlParser{src: `query { user(id: 1) { name email } }`}
	op, err := p.parseOperation()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if op.Type != "query" {
		t.Errorf("type: got %q", op.Type)
	}
	if len(op.Fields) != 1 || op.Fields[0].Name != "user" {
		t.Fatalf("fields: %+v", op.Fields)
	}
	user := op.Fields[0]
	if id, ok := user.Args["id"].(float64); !ok || id != 1 {
		t.Errorf("args: %+v", user.Args)
	}
	if len(user.Children) != 2 || user.Children[0].Name != "name" || user.Children[1].Name != "email" {
		t.Errorf("children: %+v", user.Children)
	}
}

func TestGraphQLParseAnonymousQuery(t *testing.T) {
	// `{ user { name } }` — leading `query` omitted.
	p := &gqlParser{src: `{ user(id: 1) { name } }`}
	op, err := p.parseOperation()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if op.Type != "query" {
		t.Errorf("type default: got %q", op.Type)
	}
	if len(op.Fields) != 1 {
		t.Fatalf("fields: %+v", op.Fields)
	}
}

func TestGraphQLParseMutation(t *testing.T) {
	p := &gqlParser{src: `mutation { create_user(name: "alice", age: 30) { id } }`}
	op, _ := p.parseOperation()
	if op.Type != "mutation" {
		t.Errorf("type: got %q", op.Type)
	}
	if op.Fields[0].Args["name"] != "alice" {
		t.Errorf("name arg: %+v", op.Fields[0].Args)
	}
}

func TestGraphQLParseAlias(t *testing.T) {
	p := &gqlParser{src: `query { current: me { name } }`}
	op, _ := p.parseOperation()
	f := op.Fields[0]
	if f.Alias != "current" || f.Name != "me" {
		t.Errorf("alias: %+v", f)
	}
}

func TestGraphQLEndToEndExecution(t *testing.T) {
	i := New()
	// Build a tiny resolver tree:
	//   Query { user(id) { name, posts { title } } }
	queryNS := NewOrderedMap()
	queryNS.Set("user", FunctionValue(&Function{Name: "Query.user", Native: func(_ *Interpreter, args []Value) (Value, error) {
		idArg := args[0].Object
		id, _ := idArg.Get("id")
		out := NewOrderedMap()
		out.Set("id", id)
		out.Set("name", StringValue("Jassim"))
		// Embed a posts array — User.posts resolver will be hit per-element.
		p1 := NewOrderedMap()
		p1.Set("title", StringValue("Hello"))
		p2 := NewOrderedMap()
		p2.Set("title", StringValue("World"))
		out.Set("posts", ArrayValue([]Value{ObjectValue(p1), ObjectValue(p2)}))
		return ObjectValue(out), nil
	}}))
	resolvers := NewOrderedMap()
	resolvers.Set("Query", ObjectValue(queryNS))

	handlerVal, err := builtinGraphQLHandler(i, []Value{ObjectValue(resolvers)})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	body := NewOrderedMap()
	body.Set("query", StringValue(`query { user(id: 42) { name posts { title } } }`))
	v, err := i.callFunction(nil, handlerVal.Function, []Value{ObjectValue(body)})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if v.Kind != KindObject {
		t.Fatalf("response: %+v", v)
	}
	data, _ := v.Object.Get("data")
	user, _ := data.Object.Get("user")
	name, _ := user.Object.Get("name")
	if name.String != "Jassim" {
		t.Errorf("name: got %v", name)
	}
	posts, _ := user.Object.Get("posts")
	if posts.Kind != KindArray || len(posts.Array) != 2 {
		t.Fatalf("posts: %+v", posts)
	}
	firstTitle, _ := posts.Array[0].Object.Get("title")
	if firstTitle.String != "Hello" {
		t.Errorf("title: got %v", firstTitle)
	}
}

func TestGraphQLErrorOnMissingResolver(t *testing.T) {
	i := New()
	resolvers := NewOrderedMap()
	resolvers.Set("Query", ObjectValue(NewOrderedMap()))

	handlerVal, _ := builtinGraphQLHandler(i, []Value{ObjectValue(resolvers)})
	body := NewOrderedMap()
	body.Set("query", StringValue(`{ users { id } }`))
	v, _ := i.callFunction(nil, handlerVal.Function, []Value{ObjectValue(body)})
	// Missing resolver returns null in `data.users`; not an error
	// (matches Apollo's convention — null fields, not response errors).
	if v.Kind != KindObject {
		t.Fatalf("got %+v", v)
	}
	data, _ := v.Object.Get("data")
	if u, _ := data.Object.Get("users"); u.Kind != KindNull {
		t.Errorf("expected null users, got %+v", u)
	}
}

func TestGraphQLVariables(t *testing.T) {
	i := New()
	queryNS := NewOrderedMap()
	queryNS.Set("user", FunctionValue(&Function{Name: "user", Native: func(_ *Interpreter, args []Value) (Value, error) {
		idArg := args[0].Object
		id, _ := idArg.Get("id")
		out := NewOrderedMap()
		out.Set("id", id)
		return ObjectValue(out), nil
	}}))
	resolvers := NewOrderedMap()
	resolvers.Set("Query", ObjectValue(queryNS))

	handlerVal, _ := builtinGraphQLHandler(i, []Value{ObjectValue(resolvers)})
	vars := NewOrderedMap()
	vars.Set("uid", NumberValue(99))
	body := NewOrderedMap()
	body.Set("query", StringValue(`query Q($uid: Int) { user(id: $uid) { id } }`))
	body.Set("variables", ObjectValue(vars))
	v, _ := i.callFunction(nil, handlerVal.Function, []Value{ObjectValue(body)})
	data, _ := v.Object.Get("data")
	user, _ := data.Object.Get("user")
	id, _ := user.Object.Get("id")
	if id.Number != 99 {
		t.Errorf("id: want 99, got %v", id.Number)
	}
}

func TestGraphQLParseErrorReturnsErrorEnvelope(t *testing.T) {
	i := New()
	resolvers := NewOrderedMap()
	resolvers.Set("Query", ObjectValue(NewOrderedMap()))
	handlerVal, _ := builtinGraphQLHandler(i, []Value{ObjectValue(resolvers)})
	body := NewOrderedMap()
	body.Set("query", StringValue(`{ unterminated`))
	v, _ := i.callFunction(nil, handlerVal.Function, []Value{ObjectValue(body)})
	errs, _ := v.Object.Get("errors")
	if errs.Kind != KindArray || len(errs.Array) == 0 {
		t.Fatalf("expected errors array, got %+v", v)
	}
	msg, _ := errs.Array[0].Object.Get("message")
	if !strings.Contains(msg.String, "graphql") {
		t.Errorf("expected graphql in error, got %q", msg.String)
	}
}
