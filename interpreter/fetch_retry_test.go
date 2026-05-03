package interpreter

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestFetchRetryRetries500UntilSuccess verifies the core promise:
// when the upstream returns 500 a few times then 200, fetch_retry
// keeps trying and ultimately gets the success response. Counts the
// hits to be sure we didn't miss any.
func TestFetchRetryRetries500UntilSuccess(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&hits, 1)
		if n < 3 {
			w.WriteHeader(500)
			w.Write([]byte("transient"))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	opts := NewOrderedMap()
	opts.Set("max_attempts", NumberValue(5))
	opts.Set("delay_ms", NumberValue(1)) // keep the test fast
	v, err := builtinFetchRetry(nil, []Value{StringValue(srv.URL), ObjectValue(opts)})
	if err != nil {
		t.Fatalf("fetch_retry: %v", err)
	}
	if v.Kind != KindObject {
		t.Fatalf("got %+v", v)
	}
	status, _ := v.Object.Get("status")
	if status.Number != 200 {
		t.Errorf("status: got %v, want 200", status.Number)
	}
	if got := atomic.LoadInt64(&hits); got != 3 {
		t.Errorf("hits: got %d, want 3 (2 fails + 1 success)", got)
	}
}

func TestFetchRetryReturns4xxImmediately(t *testing.T) {
	// 4xx is a client error — retrying won't help, so fetch_retry
	// must NOT retry. Verify hits == 1.
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(404)
	}))
	defer srv.Close()

	opts := NewOrderedMap()
	opts.Set("max_attempts", NumberValue(5))
	opts.Set("delay_ms", NumberValue(1))
	v, _ := builtinFetchRetry(nil, []Value{StringValue(srv.URL), ObjectValue(opts)})
	status, _ := v.Object.Get("status")
	if status.Number != 404 {
		t.Errorf("status: got %v", status.Number)
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Errorf("expected 1 hit (4xx not retried), got %d", got)
	}
}

func TestFetchRetryGivesUpAfterMaxAttempts(t *testing.T) {
	// Always-500 server should be hit exactly max_attempts times,
	// then return the last response.
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(503)
	}))
	defer srv.Close()

	opts := NewOrderedMap()
	opts.Set("max_attempts", NumberValue(3))
	opts.Set("delay_ms", NumberValue(1))
	v, _ := builtinFetchRetry(nil, []Value{StringValue(srv.URL), ObjectValue(opts)})
	if got := atomic.LoadInt64(&hits); got != 3 {
		t.Errorf("hits: got %d, want 3", got)
	}
	status, _ := v.Object.Get("status")
	if status.Number != 503 {
		t.Errorf("final status: got %v, want 503", status.Number)
	}
}
