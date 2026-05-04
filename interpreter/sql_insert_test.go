// sql_insert_test.go — focused tests for the v1.48-v1.52 object-driven
// SQL helpers: insert / upsert / update / delete / find / find_one /
// count / exists. Each test opens an in-memory SQLite DB, exercises
// one builtin, and checks behaviour. Builds only on non-WASM so the
// SQLite driver pulls in.

//go:build !js

package interpreter

import (
	"strings"
	"testing"
)

// sqlScratchDB returns a fresh in-memory SQLite handle plus a cleanup
// function. The schema has columns we need for the suite — keeps every
// test's setup boilerplate down to one line.
func sqlScratchDB(t *testing.T) (Value, func()) {
	t.Helper()
	v, err := builtinSQLOpen(nil, []Value{StringValue(":memory:")})
	if err != nil {
		t.Fatalf("sql.open: %v", err)
	}
	if _, err := builtinSQLMigrate(nil, []Value{v, ArrayValue([]Value{
		StringValue("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, role TEXT, active INTEGER)"),
	})}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return v, func() { _, _ = builtinSQLClose(nil, []Value{v}) }
}

func makeRow(pairs ...any) Value {
	o := NewOrderedMap()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), toValue(pairs[i+1]))
	}
	return ObjectValue(o)
}

func toValue(x any) Value {
	switch v := x.(type) {
	case string:
		return StringValue(v)
	case int:
		return NumberValue(float64(v))
	case float64:
		return NumberValue(v)
	case bool:
		return BoolValue(v)
	case nil:
		return NullValue()
	}
	return NullValue()
}

// no-op — sqlScratchDB now hands back the Value directly.

func TestSQLInsertSingle(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	r, err := builtinSQLInsert(nil, []Value{h, StringValue("users"),
		makeRow("name", "Ada", "role", "admin", "active", 1)})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, _ := r.Object.Get("rows_affected")
	if got.Number != 1 {
		t.Errorf("rows_affected = %v, want 1", got.Number)
	}
}

func TestSQLInsertBatched(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	rows := ArrayValue([]Value{
		makeRow("name", "Ada", "role", "admin", "active", 1),
		makeRow("name", "Linus", "role", "user", "active", 1),
		makeRow("name", "Grace", "role", "admin", "active", 0),
	})
	r, err := builtinSQLInsert(nil, []Value{h, StringValue("users"), rows})
	if err != nil {
		t.Fatalf("batch insert: %v", err)
	}
	got, _ := r.Object.Get("rows_affected")
	if got.Number != 3 {
		t.Errorf("rows_affected = %v, want 3", got.Number)
	}
}

func TestSQLInsertRejectsBadTable(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	_, err := builtinSQLInsert(nil, []Value{h, StringValue("users; drop"),
		makeRow("name", "x")})
	if err == nil || !strings.Contains(err.Error(), "plain identifier") {
		t.Errorf("expected plain-identifier error, got %v", err)
	}
}

func TestSQLInsertRejectsExtraKeysInBatch(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	rows := ArrayValue([]Value{
		makeRow("name", "Ada", "role", "admin"),
		makeRow("name", "Linus", "role", "user", "extra", "boom"),
	})
	_, err := builtinSQLInsert(nil, []Value{h, StringValue("users"), rows})
	if err == nil || !strings.Contains(err.Error(), "extra keys") {
		t.Errorf("expected extra-keys error, got %v", err)
	}
}

func TestSQLFindOne(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	_, _ = builtinSQLInsert(nil, []Value{h, StringValue("users"),
		makeRow("name", "Ada", "role", "admin", "active", 1)})
	r, err := builtinSQLFindOne(nil, []Value{h, StringValue("users"),
		makeRow("id", 1)})
	if err != nil || r.Kind != KindObject {
		t.Fatalf("find_one: %v %v", r.Kind, err)
	}
	name, _ := r.Object.Get("name")
	if name.String != "Ada" {
		t.Errorf("name = %q, want Ada", name.String)
	}
}

func TestSQLFindOneMissReturnsNull(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	r, err := builtinSQLFindOne(nil, []Value{h, StringValue("users"),
		makeRow("id", 999)})
	if err != nil {
		t.Fatalf("find_one: %v", err)
	}
	if r.Kind != KindNull {
		t.Errorf("expected null on miss, got %v", r.Kind)
	}
}

func TestSQLCount(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	rows := ArrayValue([]Value{
		makeRow("name", "a", "role", "admin", "active", 1),
		makeRow("name", "b", "role", "user", "active", 1),
		makeRow("name", "c", "role", "user", "active", 0),
	})
	_, _ = builtinSQLInsert(nil, []Value{h, StringValue("users"), rows})

	all, _ := builtinSQLCount(nil, []Value{h, StringValue("users"), makeRow()})
	if all.Number != 3 {
		t.Errorf("count all = %v, want 3", all.Number)
	}
	users, _ := builtinSQLCount(nil, []Value{h, StringValue("users"),
		makeRow("role", "user")})
	if users.Number != 2 {
		t.Errorf("count role=user = %v, want 2", users.Number)
	}
}

func TestSQLExists(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	_, _ = builtinSQLInsert(nil, []Value{h, StringValue("users"),
		makeRow("name", "Ada", "role", "admin", "active", 1)})

	hit, _ := builtinSQLExists(nil, []Value{h, StringValue("users"),
		makeRow("role", "admin")})
	if !hit.Bool {
		t.Errorf("expected exists=true for admin")
	}
	miss, _ := builtinSQLExists(nil, []Value{h, StringValue("users"),
		makeRow("role", "ghost")})
	if miss.Bool {
		t.Errorf("expected exists=false for ghost")
	}
}

func TestSQLUpdate(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	_, _ = builtinSQLInsert(nil, []Value{h, StringValue("users"),
		makeRow("name", "Ada", "role", "admin", "active", 1)})
	r, err := builtinSQLUpdate(nil, []Value{h, StringValue("users"),
		makeRow("name", "Ada Lovelace"),
		makeRow("id", 1)})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := r.Object.Get("rows_affected")
	if got.Number != 1 {
		t.Errorf("rows_affected = %v, want 1", got.Number)
	}
	one, _ := builtinSQLFindOne(nil, []Value{h, StringValue("users"),
		makeRow("id", 1)})
	name, _ := one.Object.Get("name")
	if name.String != "Ada Lovelace" {
		t.Errorf("name = %q after update, want Ada Lovelace", name.String)
	}
}

func TestSQLUpdateRejectsEmptyWhere(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	_, err := builtinSQLUpdate(nil, []Value{h, StringValue("users"),
		makeRow("active", 0),
		makeRow()})
	if err == nil || !strings.Contains(err.Error(), "every row") {
		t.Errorf("expected refuse-every-row error, got %v", err)
	}
}

func TestSQLUpdateRejectsEmptySet(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	_, err := builtinSQLUpdate(nil, []Value{h, StringValue("users"),
		makeRow(),
		makeRow("id", 1)})
	if err == nil || !strings.Contains(err.Error(), "set") {
		t.Errorf("expected empty-set error, got %v", err)
	}
}

func TestSQLDelete(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	_, _ = builtinSQLInsert(nil, []Value{h, StringValue("users"),
		ArrayValue([]Value{
			makeRow("name", "Ada", "role", "admin"),
			makeRow("name", "Linus", "role", "user"),
		})})
	r, err := builtinSQLDelete(nil, []Value{h, StringValue("users"),
		makeRow("role", "user")})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := r.Object.Get("rows_affected")
	if got.Number != 1 {
		t.Errorf("rows_affected = %v, want 1", got.Number)
	}
}

func TestSQLDeleteRejectsEmptyWhere(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	_, err := builtinSQLDelete(nil, []Value{h, StringValue("users"),
		makeRow()})
	if err == nil || !strings.Contains(err.Error(), "every row") {
		t.Errorf("expected refuse-every-row error, got %v", err)
	}
}

func TestSQLUpsertCreates(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	r, err := builtinSQLUpsert(nil, []Value{h, StringValue("users"),
		makeRow("id", 7, "name", "Ada", "role", "admin", "active", 1),
		ArrayValue([]Value{StringValue("id")})})
	if err != nil {
		t.Fatalf("upsert insert: %v", err)
	}
	got, _ := r.Object.Get("rows_affected")
	if got.Number != 1 {
		t.Errorf("rows_affected on insert = %v, want 1", got.Number)
	}
}

func TestSQLUpsertUpdatesOnConflict(t *testing.T) {
	h, cleanup := sqlScratchDB(t)
	defer cleanup()
	first := []Value{h, StringValue("users"),
		makeRow("id", 7, "name", "old", "role", "admin", "active", 1),
		ArrayValue([]Value{StringValue("id")})}
	if _, err := builtinSQLUpsert(nil, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second := []Value{h, StringValue("users"),
		makeRow("id", 7, "name", "new", "role", "user", "active", 0),
		ArrayValue([]Value{StringValue("id")})}
	if _, err := builtinSQLUpsert(nil, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	one, _ := builtinSQLFindOne(nil, []Value{h, StringValue("users"),
		makeRow("id", 7)})
	name, _ := one.Object.Get("name")
	if name.String != "new" {
		t.Errorf("name after upsert = %q, want new", name.String)
	}
	role, _ := one.Object.Get("role")
	if role.String != "user" {
		t.Errorf("role after upsert = %q, want user", role.String)
	}
}
