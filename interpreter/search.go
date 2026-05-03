//go:build !js

// search.go — SQLite FTS5 full-text search wrapper. The engine
// (FTS5) ships in the pure-Go modernc.org/sqlite driver we already
// depend on, so this namespace is zero-extra-deps. The four calls
// covered are enough for any "search bar" on top of a SQL table:
//
//	search.create(db, "posts_fts", ["title", "body"])
//	search.index(db, "posts_fts", post.id, post)
//	let hits = search.query(db, "posts_fts", "lang AND fast", { limit: 20 })
//	search.delete(db, "posts_fts", post.id)
//
// FTS5 supports BM25 ranking, NEAR proximity search, prefix queries,
// boolean operators (AND/OR/NOT), and column-scoped queries
// (`title: lang`). The wrapper passes user queries through unchanged
// so all of that works out of the box — see the FTS5 docs:
// https://www.sqlite.org/fts5.html
package interpreter

import (
	"fmt"
	"strings"
)

// search.create(db, table, columns) — create a FTS5 virtual table
// with the given columns. Idempotent: uses CREATE VIRTUAL TABLE IF
// NOT EXISTS so re-running isn't an error.
func builtinSearchCreate(_ *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 3 || args[1].Kind != KindString || args[2].Kind != KindArray {
		return Value{}, fmt.Errorf("search.create(db, table, columns[]) requires (db, string, array)")
	}
	table := args[1].String
	cols := args[2].Array
	if len(cols) == 0 {
		return Value{}, fmt.Errorf("search.create: columns must not be empty")
	}
	colNames := make([]string, len(cols))
	for k, c := range cols {
		if c.Kind != KindString {
			return Value{}, fmt.Errorf("search.create: column %d must be a string", k)
		}
		colNames[k] = c.String
	}
	stmt := fmt.Sprintf(
		"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(%s)",
		quoteIdent(table), strings.Join(colNames, ", "),
	)
	if _, err := h.runner().Exec(stmt); err != nil {
		return Value{}, err
	}
	return NullValue(), nil
}

// search.index(db, table, id, doc) — insert (or replace) a row in
// the FTS5 table. `id` is stored in the synthetic rowid column;
// `doc` is an object whose keys map onto the table's columns.
//
// Note FTS5 doesn't enforce a unique-id column the way regular
// tables do — we use UPDATE ... WHERE rowid = ? then INSERT to make
// re-indexing the same id idempotent. Callers who want uniqueness
// elsewhere should keep a separate non-FTS5 source-of-truth table.
func builtinSearchIndex(_ *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 4 || args[1].Kind != KindString || args[3].Kind != KindObject {
		return Value{}, fmt.Errorf("search.index(db, table, id, doc) requires (db, string, any, object)")
	}
	table := args[1].String
	id := args[2]
	doc := args[3].Object

	// Pull column list from the FTS5 schema so we don't have to ask
	// the user to pass it again. table_info() returns each column.
	rows, err := h.runner().Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return Value{}, err
	}
	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			cols = append(cols, name)
		}
	}
	rows.Close()
	if len(cols) == 0 {
		return Value{}, fmt.Errorf("search.index: table %q has no columns (was it created?)", table)
	}

	// Delete-then-insert keeps repeated indexing idempotent.
	if _, err := h.runner().Exec(
		fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", quoteIdent(table)),
		toGoArg(id),
	); err != nil {
		return Value{}, err
	}

	placeholders := make([]string, 1+len(cols))
	values := make([]any, 1+len(cols))
	placeholders[0] = "?"
	values[0] = toGoArg(id)
	for k, c := range cols {
		placeholders[k+1] = "?"
		v, _ := doc.Get(c)
		values[k+1] = toGoArg(v)
	}
	stmt := fmt.Sprintf(
		"INSERT INTO %s (rowid, %s) VALUES (%s)",
		quoteIdent(table),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)
	if _, err := h.runner().Exec(stmt, values...); err != nil {
		return Value{}, err
	}
	return NullValue(), nil
}

// search.query(db, table, q, opts?) — run an FTS5 MATCH query. Returns
// an array of `{ id, rank, ...columns }` ordered by relevance (FTS5
// `bm25(table)`). opts:
//
//   limit:  max rows to return (default 50)
//   offset: pagination offset (default 0)
func builtinSearchQuery(_ *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 3 || args[1].Kind != KindString || args[2].Kind != KindString {
		return Value{}, fmt.Errorf("search.query(db, table, q, opts?) requires (db, string, string)")
	}
	table := args[1].String
	q := args[2].String
	limit := 50
	offset := 0
	if len(args) > 3 && args[3].Kind == KindObject {
		if v, ok := args[3].Object.Get("limit"); ok && v.Kind == KindNumber {
			limit = int(v.Number)
		}
		if v, ok := args[3].Object.Get("offset"); ok && v.Kind == KindNumber {
			offset = int(v.Number)
		}
	}

	stmt := fmt.Sprintf(
		"SELECT rowid, *, bm25(%s) AS rank FROM %s WHERE %s MATCH ? ORDER BY rank LIMIT ? OFFSET ?",
		quoteIdent(table), quoteIdent(table), quoteIdent(table),
	)
	rows, err := h.runner().Query(stmt, q, limit, offset)
	if err != nil {
		return Value{}, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return Value{}, err
	}
	var out []Value
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return Value{}, err
		}
		om := NewOrderedMap()
		for i, c := range cols {
			if c == "rowid" {
				om.Set("id", sqlValueToMX(raw[i]))
			} else {
				om.Set(c, sqlValueToMX(raw[i]))
			}
		}
		out = append(out, ObjectValue(om))
	}
	return ArrayValue(out), nil
}

// search.delete(db, table, id) — remove a row by id.
func builtinSearchDelete(_ *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 3 || args[1].Kind != KindString {
		return Value{}, fmt.Errorf("search.delete(db, table, id) requires (db, string, any)")
	}
	stmt := fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", quoteIdent(args[1].String))
	if _, err := h.runner().Exec(stmt, toGoArg(args[2])); err != nil {
		return Value{}, err
	}
	return NullValue(), nil
}

// quoteIdent wraps a SQL identifier in double-quotes, doubling any
// embedded quotes. Used so user-supplied table names can't escape
// into the SQL outside the backtick fences we control.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// toGoArg converts a single Value to the Go-native shape sql.Exec /
// sql.Query expects — same logic as goArgs but for one value.
func toGoArg(v Value) any {
	switch v.Kind {
	case KindNull:
		return nil
	case KindBool:
		return v.Bool
	case KindNumber:
		return v.Number
	case KindString:
		return v.String
	default:
		return v.Display()
	}
}
