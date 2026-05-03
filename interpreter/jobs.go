//go:build !js

// jobs.go — durable background job queue backed by SQLite.
//
// Two-line worker:
//
//	let q = jobs.create({ db: "./jobs.db", queue: "emails" })
//	q.enqueue({ to: "alice@example.com", subject: "Hi" })
//	q.process(2, fn(job) { email.send(...job) })   # 2 workers
//
// Each job carries: id, queue, payload (JSON), status (pending |
// running | done | failed), attempts, last_error, run_at,
// created_at. Failed jobs retry with exponential backoff up to
// `max_attempts` (default 3). After that they stay marked `failed`
// in the table for inspection.
package interpreter

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

type jobsHandle struct {
	db          *sql.DB
	queue       string
	maxAttempts int
	mu          sync.Mutex // serializes claim() across workers
}

func jobsCreate(opts *OrderedMap) (*jobsHandle, error) {
	getStr := func(k string) string {
		if v, ok := opts.Get(k); ok && v.Kind == KindString {
			return v.String
		}
		return ""
	}
	dbPath := getStr("db")
	if dbPath == "" {
		dbPath = "./jobs.db"
	}
	queue := getStr("queue")
	if queue == "" {
		queue = "default"
	}
	max := 3
	if v, ok := opts.Get("max_attempts"); ok && v.Kind == KindNumber {
		max = int(v.Number)
	}

	d, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	// SQLite likes a single writer; cap connections + bump busy timeout.
	d.SetMaxOpenConns(1)
	if _, err := d.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		d.Close()
		return nil, err
	}
	if _, err := d.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		d.Close()
		return nil, err
	}
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS mx_jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		queue TEXT NOT NULL,
		payload TEXT NOT NULL,
		status TEXT NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0,
		last_error TEXT,
		run_at TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`); err != nil {
		d.Close()
		return nil, err
	}
	if _, err := d.Exec(`CREATE INDEX IF NOT EXISTS mx_jobs_pending ON mx_jobs(queue, status, run_at)`); err != nil {
		d.Close()
		return nil, err
	}
	return &jobsHandle{db: d, queue: queue, maxAttempts: max}, nil
}

func jobsEnqueue(h *jobsHandle, payload Value, delaySec float64) (int64, error) {
	encoded, err := jsonEncode(payload)
	if err != nil {
		return 0, err
	}
	runAt := time.Now().UTC().Add(time.Duration(delaySec * float64(time.Second)))
	res, err := h.db.Exec(
		`INSERT INTO mx_jobs (queue, payload, status, run_at, created_at) VALUES (?, ?, 'pending', ?, ?)`,
		h.queue,
		string(encoded),
		runAt.Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// jobsClaim atomically picks one ready job and marks it `running`.
// Returns `nil, nil` when nothing is ready.
func jobsClaim(h *jobsHandle) (*claimedJob, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	row := tx.QueryRow(`
		SELECT id, payload, attempts FROM mx_jobs
		WHERE queue = ? AND status = 'pending' AND run_at <= ?
		ORDER BY id LIMIT 1`,
		h.queue, time.Now().UTC().Format(time.RFC3339Nano),
	)
	var id int64
	var payload string
	var attempts int
	if err := row.Scan(&id, &payload, &attempts); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if _, err := tx.Exec(
		`UPDATE mx_jobs SET status = 'running', attempts = attempts + 1 WHERE id = ?`, id,
	); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &claimedJob{ID: id, Payload: payload, Attempts: attempts + 1}, nil
}

type claimedJob struct {
	ID       int64
	Payload  string
	Attempts int
}

func jobsMarkDone(h *jobsHandle, id int64) error {
	_, err := h.db.Exec(`UPDATE mx_jobs SET status = 'done' WHERE id = ?`, id)
	return err
}

func jobsMarkFailed(h *jobsHandle, id int64, attempts int, errMsg string) error {
	if attempts >= h.maxAttempts {
		_, err := h.db.Exec(
			`UPDATE mx_jobs SET status = 'failed', last_error = ? WHERE id = ?`,
			errMsg, id,
		)
		return err
	}
	// Exponential backoff: 5s, 10s, 20s, 40s, ...
	backoff := time.Duration(5*int64(1<<uint(attempts-1))) * time.Second
	runAt := time.Now().UTC().Add(backoff)
	_, err := h.db.Exec(
		`UPDATE mx_jobs SET status = 'pending', last_error = ?, run_at = ? WHERE id = ?`,
		errMsg, runAt.Format(time.RFC3339Nano), id,
	)
	return err
}

func pow5(n int) int64 {
	out := int64(1)
	for i := 0; i < n; i++ {
		out *= 5
	}
	return out
}

// jobsWorker is the goroutine body that polls + executes.
func (h *jobsHandle) startWorker(stop <-chan struct{}, fn func(payload string) error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		default:
		}
		job, err := jobsClaim(h)
		if err != nil {
			fmt.Fprintf(stderrShim(), "[mx jobs] claim error: %v\n", err)
			<-ticker.C
			continue
		}
		if job == nil {
			// Nothing to do — wait for the next tick or a stop signal.
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			continue
		}
		if err := fn(job.Payload); err != nil {
			_ = jobsMarkFailed(h, job.ID, job.Attempts, err.Error())
		} else {
			_ = jobsMarkDone(h, job.ID)
		}
	}
}

// stderrShim is a stub so jobs.go avoids importing os directly. The
// real interpreter package already pulls in os, so this is just a thin
// wrapper that compiles even if the import lands elsewhere.
func stderrShim() interface{ Write(p []byte) (int, error) } { return &globalStderr{} }

type globalStderr struct{}

func (s *globalStderr) Write(p []byte) (int, error) { return _stderrWrite(p) }

// _stderrWrite is a function variable so we can resolve to the real
// os.Stderr from interpreter.go without a circular reference.
var _stderrWrite = func(p []byte) (int, error) { return len(p), nil }

// JobsHandle is a sync.WaitGroup-tracked control object. Process
// returns a stop function that gracefully drains running goroutines.
type jobsControl struct {
	stop chan struct{}
	wg   sync.WaitGroup
}

// ===== Job-queue builtins (production impl) =====

func builtinJobsCreate(i *Interpreter, args []Value) (Value, error) {
	opts := NewOrderedMap()
	if len(args) > 0 && args[0].Kind == KindObject {
		opts = args[0].Object
	}
	h, err := jobsCreate(opts)
	if err != nil {
		return Value{}, err
	}

	q := NewOrderedMap()
	q.Set("enqueue", FunctionValue(&Function{Name: "queue.enqueue", Native: func(_ *Interpreter, a []Value) (Value, error) {
		if len(a) < 1 {
			return Value{}, fmt.Errorf("enqueue(payload, opts?) requires a payload")
		}
		delay := 0.0
		if len(a) > 1 && a[1].Kind == KindObject {
			if v, ok := a[1].Object.Get("delay_seconds"); ok && v.Kind == KindNumber {
				delay = v.Number
			}
		}
		id, err := jobsEnqueue(h, a[0], delay)
		if err != nil {
			return Value{}, err
		}
		return NumberValue(float64(id)), nil
	}}))
	q.Set("process", FunctionValue(&Function{Name: "queue.process", Native: func(interp *Interpreter, a []Value) (Value, error) {
		workers := 1
		var fn *Function
		if len(a) == 1 && a[0].Kind == KindFunction {
			fn = a[0].Function
		} else if len(a) >= 2 && a[0].Kind == KindNumber && a[1].Kind == KindFunction {
			workers = int(a[0].Number)
			fn = a[1].Function
		} else {
			return Value{}, fmt.Errorf("process(workers?, fn) requires a function")
		}
		ctrl := &jobsControl{stop: make(chan struct{})}
		for w := 0; w < workers; w++ {
			ctrl.wg.Add(1)
			go func() {
				defer ctrl.wg.Done()
				h.startWorker(ctrl.stop, func(payload string) error {
					decoded, decErr := jsonDecode([]byte(payload))
					if decErr != nil {
						decoded = StringValue(payload)
					}
					if _, err := interp.callFunction(nil, fn, []Value{decoded}); err != nil {
						return err
					}
					return nil
				})
			}()
		}
		stop := &Function{Name: "queue.stop", Native: func(_ *Interpreter, _ []Value) (Value, error) {
			select {
			case <-ctrl.stop:
			default:
				close(ctrl.stop)
			}
			ctrl.wg.Wait()
			return NullValue(), nil
		}}
		return FunctionValue(stop), nil
	}}))
	q.Set("close", FunctionValue(&Function{Name: "queue.close", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		return NullValue(), h.db.Close()
	}}))
	q.Set("stats", FunctionValue(&Function{Name: "queue.stats", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		out := NewOrderedMap()
		for _, status := range []string{"pending", "running", "done", "failed"} {
			row := h.db.QueryRow(`SELECT COUNT(*) FROM mx_jobs WHERE queue = ? AND status = ?`, h.queue, status)
			var n int
			if err := row.Scan(&n); err == nil {
				out.Set(status, NumberValue(float64(n)))
			}
		}
		return ObjectValue(out), nil
	}}))
	return ObjectValue(q), nil
}
