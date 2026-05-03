//go:build !js

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
//
// Disabled under js/wasm — the sqlite driver depends on cgo-style
// libc shims that don't build for the browser. sql_wasm.go provides
// stubs that return clear errors at runtime.
package interpreter

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // MySQL driver, registered as "mysql"
	_ "github.com/lib/pq"              // Postgres driver, registered as "postgres"
	_ "modernc.org/sqlite"             // pure-Go SQLite driver, registered as "sqlite"
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
//	"./local.db" / ":memory:"                -> SQLite
//	"postgres://..." / "postgresql://..."    -> Postgres (lib/pq)
//	"mysql://..." or "user:pass@tcp(...)/db" -> MySQL (go-sql-driver)
//
// MySQL DSNs that start with `mysql://` get the prefix stripped before
// being handed to the driver, since go-sql-driver expects the bare
// `<user>:<pass>@tcp(host:port)/db` form.
func sqlOpen(path string) (*dbHandle, error) {
	driver := "sqlite"
	dsn := path
	switch {
	case strings.HasPrefix(path, "postgres://"), strings.HasPrefix(path, "postgresql://"):
		driver = "postgres"
	case strings.HasPrefix(path, "mysql://"):
		driver = "mysql"
		dsn = strings.TrimPrefix(path, "mysql://")
	case strings.Contains(path, "@tcp(") && strings.Contains(path, ")/"):
		driver = "mysql"
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

// ===== SQL builtins (production impl) =====

func builtinSQLOpen(i *Interpreter, args []Value) (Value, error) {
	path, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	h, err := sqlOpen(path)
	if err != nil {
		return Value{}, err
	}
	return HandleValue(h), nil
}

func mustDBHandle(args []Value) (*dbHandle, error) {
	if len(args) < 1 || args[0].Kind != KindHandle {
		return nil, fmt.Errorf("expected a sql.open handle as first argument")
	}
	h, ok := args[0].Handle.(*dbHandle)
	if !ok {
		return nil, fmt.Errorf("argument is not a SQL handle")
	}
	return h, nil
}

func builtinSQLExec(i *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	query, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	return sqlExec(h, query, args[2:])
}

func builtinSQLQuery(i *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	query, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	return sqlQuery(h, query, args[2:])
}

func builtinSQLQueryOne(i *Interpreter, args []Value) (Value, error) {
	v, err := builtinSQLQuery(i, args)
	if err != nil {
		return Value{}, err
	}
	if v.Kind != KindArray || len(v.Array) == 0 {
		return NullValue(), nil
	}
	return v.Array[0], nil
}

func builtinSQLClose(i *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	return NullValue(), h.db.Close()
}

func builtinSQLMigrate(i *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 2 || args[1].Kind != KindArray {
		return Value{}, fmt.Errorf("sql.migrate(db, migrations) requires an array of SQL strings")
	}
	migrations := args[1].Array

	if _, err := h.runner().Exec(`CREATE TABLE IF NOT EXISTS mx_migrations (
		id INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL,
		hash TEXT NOT NULL
	)`); err != nil {
		return Value{}, err
	}

	rows, err := h.runner().Query("SELECT id, hash FROM mx_migrations ORDER BY id")
	if err != nil {
		return Value{}, err
	}
	applied := map[int]string{}
	for rows.Next() {
		var id int
		var hash string
		if err := rows.Scan(&id, &hash); err != nil {
			rows.Close()
			return Value{}, err
		}
		applied[id] = hash
	}
	rows.Close()

	var ranList, skippedList []Value
	for k, m := range migrations {
		if m.Kind != KindString {
			return Value{}, fmt.Errorf("sql.migrate: each migration must be a string (got %s)", m.typeName())
		}
		hsh := computeHMACHex("mx-migrations", m.String)
		if existing, ok := applied[k+1]; ok {
			if existing != hsh {
				return Value{}, fmt.Errorf("sql.migrate: migration #%d has been edited since it was applied (hash mismatch)", k+1)
			}
			skippedList = append(skippedList, NumberValue(float64(k+1)))
			continue
		}
		if _, err := h.runner().Exec(m.String); err != nil {
			return Value{}, fmt.Errorf("sql.migrate #%d failed: %w", k+1, err)
		}
		if _, err := h.runner().Exec(
			"INSERT INTO mx_migrations (id, applied_at, hash) VALUES (?, ?, ?)",
			k+1, time.Now().UTC().Format(time.RFC3339), hsh,
		); err != nil {
			return Value{}, err
		}
		ranList = append(ranList, NumberValue(float64(k+1)))
	}

	out := NewOrderedMap()
	out.Set("applied", ArrayValue(ranList))
	out.Set("skipped", ArrayValue(skippedList))
	return ObjectValue(out), nil
}

func builtinSQLTransaction(i *Interpreter, args []Value) (Value, error) {
	h, err := mustDBHandle(args)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 2 || args[1].Kind != KindFunction {
		return Value{}, fmt.Errorf("sql.transaction(db, fn) requires a function")
	}
	tx, err := h.db.Begin()
	if err != nil {
		return Value{}, err
	}
	txHandle := &dbHandle{db: nil, tx: tx}
	v, callErr := i.callFunction(nil, args[1].Function, []Value{HandleValue(txHandle)})
	if callErr != nil {
		_ = tx.Rollback()
		return Value{}, callErr
	}
	if err := tx.Commit(); err != nil {
		return Value{}, err
	}
	return v, nil
}
