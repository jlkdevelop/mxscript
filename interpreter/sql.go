// sql.go — SQLite via the pure-Go modernc.org/sqlite driver. Exposed
// through the `sql` namespace:
//
//	let db = sql.open("./data.db")           # opens / creates a database
//	sql.exec(db, "CREATE TABLE users (...)")
//	sql.exec(db, "INSERT ... VALUES (?, ?)", "Jassim", 30)
//	let rows = sql.query(db, "SELECT * FROM users WHERE name = ?", "Jassim")
//	loop rows as u { print(u.name) }
//	let one = sql.query_one(db, "SELECT * FROM users WHERE id = ?", 1)
//	sql.close(db)
package interpreter

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/lib/pq"  // Postgres driver, registered as "postgres"
	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

// dbHandle is the opaque value handed to .mx code. It carries either a
// connection pool (`db`) or an in-flight transaction (`tx`); the
// `runner` shim picks whichever is set.
type dbHandle struct {
	db *sql.DB
	tx *sql.Tx
}

// runner is the subset of *sql.DB / *sql.Tx that we need. Picking the
// non-nil one lets sql.exec / sql.query work both inside and outside
// transactions without duplicating code.
type sqlRunner interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
}

func (h *dbHandle) runner() sqlRunner {
	if h.tx != nil {
		return h.tx
	}
	return h.db
}

// sqlOpen picks a driver from the DSN shape:
//
//	"./local.db"                            -> SQLite
//	"file:..." or any plain path / :memory: -> SQLite
//	"postgres://..." or "postgresql://..."  -> Postgres (lib/pq)
//
// Future drivers can be added by extending the switch — every dep is
// imported as a side-effect (database/sql.Register-driven).
func sqlOpen(path string) (*dbHandle, error) {
	driver := "sqlite"
	dsn := path
	switch {
	case strings.HasPrefix(path, "postgres://"), strings.HasPrefix(path, "postgresql://"):
		driver = "postgres"
	}
	d, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	return &dbHandle{db: d}, nil
}

// goArgs unwraps Value args into native Go types the driver expects.
func goArgs(args []Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		switch a.Kind {
		case KindNull:
			out[i] = nil
		case KindBool:
			out[i] = a.Bool
		case KindNumber:
			out[i] = a.Number
		case KindString:
			out[i] = a.String
		default:
			// Fall back to Display() for arrays/objects (JSON string).
			out[i] = a.Display()
		}
	}
	return out
}

// sqlExec runs INSERT/UPDATE/DELETE/CREATE statements. Returns
// { rows_affected, last_insert_id }.
func sqlExec(h *dbHandle, query string, args []Value) (Value, error) {
	res, err := h.runner().Exec(query, goArgs(args)...)
	if err != nil {
		return Value{}, err
	}
	out := NewOrderedMap()
	if affected, err := res.RowsAffected(); err == nil {
		out.Set("rows_affected", NumberValue(float64(affected)))
	}
	if id, err := res.LastInsertId(); err == nil {
		out.Set("last_insert_id", NumberValue(float64(id)))
	}
	return ObjectValue(out), nil
}

// sqlQuery runs SELECT statements. Returns an array of objects, each
// with column-name keys.
func sqlQuery(h *dbHandle, query string, args []Value) (Value, error) {
	rows, err := h.runner().Query(query, goArgs(args)...)
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
			om.Set(c, sqlValueToMX(raw[i]))
		}
		out = append(out, ObjectValue(om))
	}
	if err := rows.Err(); err != nil {
		return Value{}, err
	}
	return ArrayValue(out), nil
}

// sqlValueToMX converts a Go value (as scanned from a sql.Row) into an MX value.
func sqlValueToMX(v any) Value {
	switch x := v.(type) {
	case nil:
		return NullValue()
	case bool:
		return BoolValue(x)
	case int64:
		return NumberValue(float64(x))
	case float64:
		return NumberValue(x)
	case string:
		return StringValue(x)
	case []byte:
		return StringValue(string(x))
	}
	return StringValue(fmt.Sprintf("%v", v))
}
