// sql_insert.go — high-level INSERT helpers.
//
// `sql.exec` works fine for hand-written INSERTs but every API ends up
// reinventing the same code: take an object, pull keys, build
// "(?, ?, ?)", thread the values into args. Annoying. Errors creep in
// when columns drift.
//
// `sql.insert` collapses that:
//
//   sql.insert(db, "users", { name: "Ada", role: "admin" })
//
//   sql.insert(db, "users", [
//     { name: "Ada",   role: "admin" },
//     { name: "Linus", role: "user"  }
//   ])
//
// First form INSERTs one row. Second form does a batched multi-row
// INSERT with a single round-trip. Returns the same shape as sql.exec
// — `{ rows_affected, last_insert_id }`.
//
// Build tag matches the rest of sql.go so a WASM build still links.

//go:build !js

package interpreter

import (
	"fmt"
	"sort"
	"strings"
)

func builtinSQLInsert(_ *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 3 {
		return Value{}, fmt.Errorf("sql.insert(db, table, row|rows) requires 3 arguments")
	}
	table, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	if !validIdent(table) {
		return Value{}, fmt.Errorf("sql.insert: table name %q must be a plain identifier", table)
	}

	// Normalise to a slice of rows.
	var rows []*OrderedMap
	switch args[2].Kind {
	case KindObject:
		rows = []*OrderedMap{args[2].Object}
	case KindArray:
		for i, v := range args[2].Array {
			if v.Kind != KindObject {
				return Value{}, fmt.Errorf("sql.insert: row %d must be an object, got %s", i, v.typeName())
			}
			rows = append(rows, v.Object)
		}
	default:
		return Value{}, fmt.Errorf("sql.insert: third arg must be an object or array of objects")
	}
	if len(rows) == 0 {
		out := NewOrderedMap()
		out.Set("rows_affected", NumberValue(0))
		return ObjectValue(out), nil
	}

	// Use the first row's keys as the canonical column list. Subsequent
	// rows that omit a column get NULL; rows with extra keys are an
	// error so typos get caught instead of silently dropping data.
	cols := append([]string(nil), rows[0].Keys...)
	sort.Strings(cols)
	for i, r := range rows[1:] {
		extras := []string{}
		for _, k := range r.Keys {
			found := false
			for _, c := range cols {
				if c == k {
					found = true
					break
				}
			}
			if !found {
				extras = append(extras, k)
			}
		}
		if len(extras) > 0 {
			return Value{}, fmt.Errorf("sql.insert: row %d has extra keys %v not in row 0", i+1, extras)
		}
	}
	for _, c := range cols {
		if !validIdent(c) {
			return Value{}, fmt.Errorf("sql.insert: column name %q must be a plain identifier", c)
		}
	}

	// Build "(?, ?, ?), (?, ?, ?), ..." and the flat arg list.
	rowPlaceholder := "(" + strings.Repeat("?, ", len(cols)-1) + "?)"
	allRows := make([]string, len(rows))
	for i := range rows {
		allRows[i] = rowPlaceholder
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
		table,
		strings.Join(cols, ", "),
		strings.Join(allRows, ", "),
	)
	flat := make([]Value, 0, len(cols)*len(rows))
	for _, r := range rows {
		for _, c := range cols {
			v, ok := r.Get(c)
			if !ok {
				flat = append(flat, NullValue())
				continue
			}
			flat = append(flat, v)
		}
	}
	return sqlExec(h, q, flat)
}

// validIdent matches identifiers safe to interpolate into SQL — letters,
// digits, underscore, no embedded quotes or whitespace. We don't quote
// table/column names because the supported SQLite/Postgres/MySQL trio
// each have their own quoting rules; demanding plain identifiers
// sidesteps that without losing 99% of real-world use.
func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
