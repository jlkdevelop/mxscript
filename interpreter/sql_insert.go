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

// sql.upsert(db, table, row, conflict_keys) — INSERT or UPDATE.
//
//   sql.upsert(db, "users", { id: 1, name: "Ada", email: "ada@x.com" }, ["id"])
//   sql.upsert(db, "settings", { user_id: 7, key: "theme", value: "dark" }, ["user_id", "key"])
//
// Picks the right dialect from the connection driver:
//   • sqlite + postgres → INSERT ... ON CONFLICT (cols) DO UPDATE SET col = excluded.col
//   • mysql            → INSERT ... ON DUPLICATE KEY UPDATE col = VALUES(col)
//
// `conflict_keys` is the unique-constraint column list. Non-key columns
// are updated to the new value on conflict; key columns are left alone
// (re-assigning them to themselves would be a no-op anyway).
func builtinSQLUpsert(_ *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 4 {
		return Value{}, fmt.Errorf("sql.upsert(db, table, row, conflict_keys) requires 4 arguments")
	}
	table, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	if !validIdent(table) {
		return Value{}, fmt.Errorf("sql.upsert: table name %q must be a plain identifier", table)
	}
	if args[2].Kind != KindObject {
		return Value{}, fmt.Errorf("sql.upsert: third arg must be a row object")
	}
	if args[3].Kind != KindArray {
		return Value{}, fmt.Errorf("sql.upsert: fourth arg must be an array of conflict-key column names")
	}
	row := args[2].Object
	cols := append([]string(nil), row.Keys...)
	sort.Strings(cols)
	for _, c := range cols {
		if !validIdent(c) {
			return Value{}, fmt.Errorf("sql.upsert: column name %q must be a plain identifier", c)
		}
	}

	conflictKeys := make([]string, 0, len(args[3].Array))
	keySet := map[string]bool{}
	for i, v := range args[3].Array {
		if v.Kind != KindString || !validIdent(v.String) {
			return Value{}, fmt.Errorf("sql.upsert: conflict_keys[%d] must be a plain identifier string", i)
		}
		conflictKeys = append(conflictKeys, v.String)
		keySet[v.String] = true
	}
	if len(conflictKeys) == 0 {
		return Value{}, fmt.Errorf("sql.upsert: conflict_keys cannot be empty")
	}

	// Build the value-bind args once; both dialects use the same set.
	flat := make([]Value, 0, len(cols))
	for _, c := range cols {
		v, ok := row.Get(c)
		if !ok {
			flat = append(flat, NullValue())
			continue
		}
		flat = append(flat, v)
	}
	placeholders := "(" + strings.Repeat("?, ", len(cols)-1) + "?)"

	// Non-key columns get UPDATE-on-conflict; key columns are skipped
	// because setting them to themselves is a no-op.
	updateCols := make([]string, 0, len(cols))
	for _, c := range cols {
		if !keySet[c] {
			updateCols = append(updateCols, c)
		}
	}

	var query string
	switch h.driver {
	case "mysql":
		// ON DUPLICATE KEY UPDATE col = VALUES(col)
		var setExprs []string
		for _, c := range updateCols {
			setExprs = append(setExprs, fmt.Sprintf("%s = VALUES(%s)", c, c))
		}
		clause := ""
		if len(setExprs) > 0 {
			clause = " ON DUPLICATE KEY UPDATE " + strings.Join(setExprs, ", ")
		}
		query = fmt.Sprintf("INSERT INTO %s (%s) VALUES %s%s",
			table, strings.Join(cols, ", "), placeholders, clause)
	default:
		// sqlite + postgres share the standard form.
		var setExprs []string
		for _, c := range updateCols {
			setExprs = append(setExprs, fmt.Sprintf("%s = excluded.%s", c, c))
		}
		clause := fmt.Sprintf(" ON CONFLICT (%s) DO ", strings.Join(conflictKeys, ", "))
		if len(setExprs) > 0 {
			clause += "UPDATE SET " + strings.Join(setExprs, ", ")
		} else {
			clause += "NOTHING"
		}
		query = fmt.Sprintf("INSERT INTO %s (%s) VALUES %s%s",
			table, strings.Join(cols, ", "), placeholders, clause)
	}
	return sqlExec(h, query, flat)
}

// sql.update(db, table, set, where) — UPDATE table SET col = ? WHERE col = ?
//
//   sql.update(db, "users", { name: "Ada", role: "admin" }, { id: 1 })
//
// `set` is the columns to change; `where` is the columns to match (AND-joined).
// Both must be objects. Empty `set` errors out so a typo doesn't accidentally
// no-op every row in the table. Empty `where` is also rejected — a bare
// UPDATE with no WHERE is almost always a bug; users who really want to
// rewrite a whole column should drop down to sql.exec.
func builtinSQLUpdate(_ *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 4 {
		return Value{}, fmt.Errorf("sql.update(db, table, set, where) requires 4 arguments")
	}
	table, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	if !validIdent(table) {
		return Value{}, fmt.Errorf("sql.update: table name %q must be a plain identifier", table)
	}
	if args[2].Kind != KindObject {
		return Value{}, fmt.Errorf("sql.update: 'set' (3rd arg) must be an object")
	}
	if args[3].Kind != KindObject {
		return Value{}, fmt.Errorf("sql.update: 'where' (4th arg) must be an object")
	}
	setObj := args[2].Object
	whereObj := args[3].Object
	if len(setObj.Keys) == 0 {
		return Value{}, fmt.Errorf("sql.update: 'set' cannot be empty")
	}
	if len(whereObj.Keys) == 0 {
		return Value{}, fmt.Errorf("sql.update: 'where' cannot be empty (refusing to update every row)")
	}

	setCols := append([]string(nil), setObj.Keys...)
	sort.Strings(setCols)
	whereCols := append([]string(nil), whereObj.Keys...)
	sort.Strings(whereCols)
	for _, c := range append(setCols, whereCols...) {
		if !validIdent(c) {
			return Value{}, fmt.Errorf("sql.update: column name %q must be a plain identifier", c)
		}
	}

	var setExprs, whereExprs []string
	flat := make([]Value, 0, len(setCols)+len(whereCols))
	for _, c := range setCols {
		setExprs = append(setExprs, c+" = ?")
		v, _ := setObj.Get(c)
		flat = append(flat, v)
	}
	for _, c := range whereCols {
		whereExprs = append(whereExprs, c+" = ?")
		v, _ := whereObj.Get(c)
		flat = append(flat, v)
	}
	q := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		table,
		strings.Join(setExprs, ", "),
		strings.Join(whereExprs, " AND "))
	return sqlExec(h, q, flat)
}

// sql.delete(db, table, where) — DELETE FROM table WHERE col = ?
//
//   sql.delete(db, "sessions", { user_id: 7 })
//   sql.delete(db, "events", { type: "trial", expired: true })
//
// `where` is an object of column-equality matches, AND-joined. Empty
// `where` is rejected — same reasoning as sql.update.
func builtinSQLDelete(_ *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 3 {
		return Value{}, fmt.Errorf("sql.delete(db, table, where) requires 3 arguments")
	}
	table, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	if !validIdent(table) {
		return Value{}, fmt.Errorf("sql.delete: table name %q must be a plain identifier", table)
	}
	if args[2].Kind != KindObject {
		return Value{}, fmt.Errorf("sql.delete: 'where' (3rd arg) must be an object")
	}
	whereObj := args[2].Object
	if len(whereObj.Keys) == 0 {
		return Value{}, fmt.Errorf("sql.delete: 'where' cannot be empty (refusing to delete every row)")
	}
	whereCols := append([]string(nil), whereObj.Keys...)
	sort.Strings(whereCols)
	for _, c := range whereCols {
		if !validIdent(c) {
			return Value{}, fmt.Errorf("sql.delete: column name %q must be a plain identifier", c)
		}
	}
	var whereExprs []string
	flat := make([]Value, 0, len(whereCols))
	for _, c := range whereCols {
		whereExprs = append(whereExprs, c+" = ?")
		v, _ := whereObj.Get(c)
		flat = append(flat, v)
	}
	q := fmt.Sprintf("DELETE FROM %s WHERE %s",
		table,
		strings.Join(whereExprs, " AND "))
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
