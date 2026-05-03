package interpreter

import "testing"

func TestFormatBracePositional(t *testing.T) {
	v, err := builtinFormat(nil, []Value{
		StringValue("Hello {}, age {}"),
		StringValue("alice"),
		NumberValue(30),
	})
	if err != nil || v.String != "Hello alice, age 30" {
		t.Errorf("got %q (err=%v)", v.String, err)
	}
}

func TestFormatBraceIndexed(t *testing.T) {
	v, _ := builtinFormat(nil, []Value{
		StringValue("{0}-{1}-{0}"),
		StringValue("a"),
		StringValue("b"),
	})
	if v.String != "a-b-a" {
		t.Errorf("got %q", v.String)
	}
}

func TestFormatDebugPlaceholder(t *testing.T) {
	om := NewOrderedMap()
	om.Set("name", StringValue("alice"))
	v, _ := builtinFormat(nil, []Value{
		StringValue("{:?}"),
		ObjectValue(om),
	})
	// Pretty form is multi-line; just confirm both pieces are in it.
	if v.String == "" || !strContains(v.String, "alice") || !strContains(v.String, "name") {
		t.Errorf("debug repr missing fields: %q", v.String)
	}
}

func TestFormatEscapedBraces(t *testing.T) {
	v, _ := builtinFormat(nil, []Value{
		StringValue("{{ {} }}"),
		StringValue("hi"),
	})
	if v.String != "{ hi }" {
		t.Errorf("got %q", v.String)
	}
}

// Printf fallback must keep working unchanged for callers that haven't
// migrated to {} placeholders yet.
func TestFormatPrintfFallback(t *testing.T) {
	v, _ := builtinFormat(nil, []Value{
		StringValue("%s = %d"),
		StringValue("n"),
		NumberValue(7),
	})
	if v.String != "n = 7" {
		t.Errorf("got %q", v.String)
	}
}

func TestFormatMissingArgIsEmpty(t *testing.T) {
	// Out-of-range placeholder positions render as empty strings,
	// matching Python .format()'s permissive handling.
	v, _ := builtinFormat(nil, []Value{
		StringValue("a={} b={}"),
		StringValue("x"),
	})
	if v.String != "a=x b=" {
		t.Errorf("got %q", v.String)
	}
}

func strContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
