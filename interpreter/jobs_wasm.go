//go:build js

// jobs_wasm.go — js/wasm stubs for the durable-jobs queue. The real
// implementation in jobs.go uses SQLite (which is gated to !js) so
// the browser build returns clear errors instead of breaking the
// interpreter package's link step.
package interpreter

import "fmt"

type jobsHandle struct{}
type claimedJob struct {
	ID       int64
	Payload  string
	Attempts int
}

type jobsControl struct{}

var errJobsUnsupported = fmt.Errorf("jobs is unsupported on the wasm build (no sqlite driver in the browser)")

func jobsCreate(opts *OrderedMap) (*jobsHandle, error) { return nil, errJobsUnsupported }
func jobsEnqueue(h *jobsHandle, payload Value, delaySec float64) (int64, error) {
	return 0, errJobsUnsupported
}
func jobsClaim(h *jobsHandle) (*claimedJob, error)                              { return nil, errJobsUnsupported }
func jobsMarkDone(h *jobsHandle, id int64) error                                { return errJobsUnsupported }
func jobsMarkFailed(h *jobsHandle, id int64, attempts int, errMsg string) error { return errJobsUnsupported }
func (h *jobsHandle) startWorker(stop <-chan struct{}, fn func(payload string) error) {
	// no-op on wasm
}

// stderrShim mirrors the production helper so callers compile.
var _stderrWrite = func(p []byte) (int, error) { return len(p), nil }

func stderrShim() interface{ Write(p []byte) (int, error) } { return _wasmStderr{} }

type _wasmStderr struct{}

func (_wasmStderr) Write(p []byte) (int, error) { return _stderrWrite(p) }

func builtinJobsCreate(i *Interpreter, args []Value) (Value, error) {
	return Value{}, errJobsUnsupported
}
