package interpreter

import (
	"strings"
	"testing"
)

func TestIDULIDShape(t *testing.T) {
	v, err := builtinIDULID(nil, nil)
	if err != nil {
		t.Fatalf("ulid: %v", err)
	}
	if len(v.String) != 26 {
		t.Errorf("ulid len: got %d, want 26", len(v.String))
	}
	for _, c := range v.String {
		if !strings.ContainsRune(crockfordBase32, c) {
			t.Errorf("ulid contains invalid char %c", c)
			break
		}
	}
}

func TestIDULIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for k := 0; k < 100; k++ {
		v, _ := builtinIDULID(nil, nil)
		if seen[v.String] {
			t.Errorf("duplicate ULID: %s", v.String)
			return
		}
		seen[v.String] = true
	}
}

func TestIDNanoIDDefaultLength(t *testing.T) {
	v, _ := builtinIDNanoID(nil, nil)
	if len(v.String) != 21 {
		t.Errorf("nanoid default: got %d, want 21", len(v.String))
	}
}

func TestIDNanoIDCustomLength(t *testing.T) {
	v, _ := builtinIDNanoID(nil, []Value{NumberValue(10)})
	if len(v.String) != 10 {
		t.Errorf("nanoid len=10: got %d", len(v.String))
	}
	for _, c := range v.String {
		if !strings.ContainsRune(nanoidAlphabet, c) {
			t.Errorf("nanoid contains invalid char %c", c)
			break
		}
	}
}

func TestIDShortIs8Chars(t *testing.T) {
	v, _ := builtinIDShort(nil, nil)
	if len(v.String) != 8 {
		t.Errorf("short: got %d, want 8", len(v.String))
	}
}

func TestIDSnowflakeIsNumericString(t *testing.T) {
	v, _ := builtinIDSnowflake(nil, nil)
	if v.Kind != KindString {
		t.Fatalf("got %v", v.Kind)
	}
	for _, c := range v.String {
		if c < '0' || c > '9' {
			t.Errorf("snowflake should be all digits, got %s", v.String)
			break
		}
	}
}

func TestPickKeepsOnlyListedKeys(t *testing.T) {
	user := NewOrderedMap()
	user.Set("id", NumberValue(1))
	user.Set("name", StringValue("Jassim"))
	user.Set("password_hash", StringValue("secret"))
	keys := ArrayValue([]Value{StringValue("id"), StringValue("name")})

	v, _ := builtinPick(nil, []Value{ObjectValue(user), keys})
	if v.Kind != KindObject {
		t.Fatalf("got %v", v)
	}
	if _, ok := v.Object.Get("password_hash"); ok {
		t.Error("pick should not include password_hash")
	}
	if name, _ := v.Object.Get("name"); name.String != "Jassim" {
		t.Errorf("name: got %v", name)
	}
}

func TestOmitDropsListedKeys(t *testing.T) {
	user := NewOrderedMap()
	user.Set("id", NumberValue(1))
	user.Set("name", StringValue("Jassim"))
	user.Set("password_hash", StringValue("secret"))
	keys := ArrayValue([]Value{StringValue("password_hash")})

	v, _ := builtinOmit(nil, []Value{ObjectValue(user), keys})
	if _, ok := v.Object.Get("password_hash"); ok {
		t.Error("omit should drop password_hash")
	}
	if id, _ := v.Object.Get("id"); id.Number != 1 {
		t.Errorf("id: got %v", id)
	}
}

func TestMergeShallow(t *testing.T) {
	a := NewOrderedMap()
	a.Set("a", NumberValue(1))
	a.Set("b", NumberValue(2))
	b := NewOrderedMap()
	b.Set("b", NumberValue(20))
	b.Set("c", NumberValue(3))

	v, _ := builtinMerge(nil, []Value{ObjectValue(a), ObjectValue(b)})
	if av, _ := v.Object.Get("a"); av.Number != 1 {
		t.Errorf("a: got %v", av)
	}
	if bv, _ := v.Object.Get("b"); bv.Number != 20 {
		t.Errorf("b should win: got %v", bv)
	}
	if cv, _ := v.Object.Get("c"); cv.Number != 3 {
		t.Errorf("c: got %v", cv)
	}
}

func TestDeepMergeRecursive(t *testing.T) {
	// defaults: { db: { host: "x", port: 5432 }, debug: false }
	defaultsDB := NewOrderedMap()
	defaultsDB.Set("host", StringValue("x"))
	defaultsDB.Set("port", NumberValue(5432))
	defaults := NewOrderedMap()
	defaults.Set("db", ObjectValue(defaultsDB))
	defaults.Set("debug", BoolValue(false))

	// override: { db: { host: "y" }, debug: true }
	overrideDB := NewOrderedMap()
	overrideDB.Set("host", StringValue("y"))
	override := NewOrderedMap()
	override.Set("db", ObjectValue(overrideDB))
	override.Set("debug", BoolValue(true))

	v, _ := builtinDeepMerge(nil, []Value{ObjectValue(defaults), ObjectValue(override)})
	merged := v.Object
	dbVal, _ := merged.Get("db")
	host, _ := dbVal.Object.Get("host")
	port, _ := dbVal.Object.Get("port")
	if host.String != "y" {
		t.Errorf("db.host: got %v, want y", host)
	}
	if port.Number != 5432 {
		t.Errorf("db.port should survive: got %v", port)
	}
	if debug, _ := merged.Get("debug"); !debug.Bool {
		t.Errorf("debug: got %v", debug)
	}
}
