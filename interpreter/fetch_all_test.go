package interpreter

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchAllReturnsResultsInInputOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo the path so we can assert ordering.
		fmt.Fprintf(w, "got %s", r.URL.Path)
	}))
	defer srv.Close()

	urls := ArrayValue([]Value{
		StringValue(srv.URL + "/a"),
		StringValue(srv.URL + "/b"),
		StringValue(srv.URL + "/c"),
	})
	v, err := builtinFetchAll(nil, []Value{urls})
	if err != nil {
		t.Fatalf("fetch_all: %v", err)
	}
	if len(v.Array) != 3 {
		t.Fatalf("got %d, want 3", len(v.Array))
	}
	for i, want := range []string{"got /a", "got /b", "got /c"} {
		text, _ := v.Array[i].Object.Get("text")
		if text.String != want {
			t.Errorf("entry %d: got %q, want %q", i, text.String, want)
		}
	}
}

func TestFetchAllErrorEntryHasErrorField(t *testing.T) {
	urls := ArrayValue([]Value{
		StringValue("not://a/valid/url"),
	})
	v, _ := builtinFetchAll(nil, []Value{urls})
	if len(v.Array) != 1 {
		t.Fatalf("got %d entries", len(v.Array))
	}
	errVal, ok := v.Array[0].Object.Get("error")
	if !ok || errVal.Kind != KindString {
		t.Errorf("expected error string, got %+v", v.Array[0])
	}
	status, _ := v.Array[0].Object.Get("status")
	if status.Number != 0 {
		t.Errorf("error entry status: got %v, want 0", status.Number)
	}
}

func TestFetchAllConcurrencyCap(t *testing.T) {
	// Track concurrent in-flight requests; with concurrency=2 we
	// should never see more than 2 simultaneously.
	var inFlight int64
	var maxSeen int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt64(&inFlight, 1)
		for {
			old := atomic.LoadInt64(&maxSeen)
			if cur <= old || atomic.CompareAndSwapInt64(&maxSeen, old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt64(&inFlight, -1)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	urls := []Value{}
	for i := 0; i < 10; i++ {
		urls = append(urls, StringValue(srv.URL+"/"+fmt.Sprint(i)))
	}
	opts := NewOrderedMap()
	opts.Set("concurrency", NumberValue(2))
	if _, err := builtinFetchAll(nil, []Value{ArrayValue(urls), ObjectValue(opts)}); err != nil {
		t.Fatalf("fetch_all: %v", err)
	}
	if atomic.LoadInt64(&maxSeen) > 2 {
		t.Errorf("concurrency cap leaked: peak %d", atomic.LoadInt64(&maxSeen))
	}
}

func TestFetchAllPostBodies(t *testing.T) {
	// Object-style entries can carry method + body.
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, r.ContentLength)
		r.Body.Read(raw)
		lastBody = string(raw)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	entry := NewOrderedMap()
	entry.Set("url", StringValue(srv.URL))
	entry.Set("method", StringValue("POST"))
	entry.Set("body", StringValue(`{"hello":"world"}`))

	v, err := builtinFetchAll(nil, []Value{ArrayValue([]Value{ObjectValue(entry)})})
	if err != nil {
		t.Fatalf("fetch_all: %v", err)
	}
	status, _ := v.Array[0].Object.Get("status")
	if status.Number != 200 {
		t.Errorf("status: %v", status.Number)
	}
	if !strings.Contains(lastBody, `"hello":"world"`) {
		t.Errorf("body: %q", lastBody)
	}
}
