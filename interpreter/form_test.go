package interpreter

import "testing"

func TestFormParseSingleValues(t *testing.T) {
	v, err := builtinFormParse(nil, []Value{StringValue("a=1&b=hello%20world&c=true")})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Kind != KindObject {
		t.Fatalf("got %+v", v)
	}
	cases := map[string]string{"a": "1", "b": "hello world", "c": "true"}
	for k, want := range cases {
		got, _ := v.Object.Get(k)
		if got.String != want {
			t.Errorf("%s: got %q, want %q", k, got.String, want)
		}
	}
}

func TestFormParseMultiValueAsArray(t *testing.T) {
	v, _ := builtinFormParse(nil, []Value{StringValue("tags=a&tags=b&tags=c")})
	tags, _ := v.Object.Get("tags")
	if tags.Kind != KindArray || len(tags.Array) != 3 {
		t.Fatalf("got %+v", tags)
	}
	if tags.Array[0].String != "a" || tags.Array[2].String != "c" {
		t.Errorf("tags: %+v", tags.Array)
	}
}

func TestFormParseMalformedReturnsNull(t *testing.T) {
	// Go's ParseQuery is permissive — most "malformed" input still
	// parses. Use a literal `%` that's not a valid escape.
	v, _ := builtinFormParse(nil, []Value{StringValue("a=%ZZ")})
	if v.Kind != KindNull {
		t.Errorf("expected null on malformed input, got %+v", v)
	}
}

func TestFormEncodeSortedDeterministic(t *testing.T) {
	obj := NewOrderedMap()
	obj.Set("z", StringValue("last"))
	obj.Set("a", StringValue("first"))
	obj.Set("m", StringValue("middle"))
	v, _ := builtinFormEncode(nil, []Value{ObjectValue(obj)})
	if v.String != "a=first&m=middle&z=last" {
		t.Errorf("got %q", v.String)
	}
}

func TestFormEncodeArrayExpansion(t *testing.T) {
	obj := NewOrderedMap()
	obj.Set("tags", ArrayValue([]Value{StringValue("a"), StringValue("b")}))
	v, _ := builtinFormEncode(nil, []Value{ObjectValue(obj)})
	if v.String != "tags=a&tags=b" {
		t.Errorf("got %q", v.String)
	}
}

func TestFormEncodeSkipsNull(t *testing.T) {
	obj := NewOrderedMap()
	obj.Set("set", StringValue("x"))
	obj.Set("missing", NullValue())
	v, _ := builtinFormEncode(nil, []Value{ObjectValue(obj)})
	if v.String != "set=x" {
		t.Errorf("null should be skipped, got %q", v.String)
	}
}

func TestFormRoundtrip(t *testing.T) {
	obj := NewOrderedMap()
	obj.Set("user", StringValue("alice"))
	obj.Set("count", NumberValue(3))
	encoded, _ := builtinFormEncode(nil, []Value{ObjectValue(obj)})
	parsed, _ := builtinFormParse(nil, []Value{encoded})
	user, _ := parsed.Object.Get("user")
	count, _ := parsed.Object.Get("count")
	if user.String != "alice" {
		t.Errorf("user: %v", user)
	}
	if count.String != "3" { // numbers stringify in form encoding
		t.Errorf("count: %v", count)
	}
}
