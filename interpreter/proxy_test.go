package interpreter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildRequestObj creates the MX-shaped request value the proxy()
// builtin expects, mirroring what an actual route handler would
// receive in `request`.
func buildRequestObj(method, path string, headers map[string]string, body string) Value {
	r := NewOrderedMap()
	r.Set("method", StringValue(method))
	r.Set("path", StringValue(path))
	r.Set("body_text", StringValue(body))

	q := NewOrderedMap()
	r.Set("query", ObjectValue(q))

	hdrs := NewOrderedMap()
	for k, v := range headers {
		hdrs.Set(k, StringValue(v))
	}
	r.Set("headers", ObjectValue(hdrs))
	return ObjectValue(r)
}

func TestProxyForwardsMethodPathBody(t *testing.T) {
	var lastMethod, lastPath, lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastMethod = r.Method
		lastPath = r.URL.Path
		raw := make([]byte, r.ContentLength)
		r.Body.Read(raw)
		lastBody = string(raw)
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(201)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	req := buildRequestObj("POST", "/api/users", map[string]string{
		"X-Auth": "Bearer token",
	}, `{"name":"jassim"}`)

	v, err := builtinProxy(nil, []Value{StringValue(srv.URL), req})
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if v.Kind != KindResponse {
		t.Fatalf("got %v", v.Kind)
	}
	if v.Response.Status != 201 {
		t.Errorf("status: %d", v.Response.Status)
	}
	if !strings.Contains(v.Response.Body.String, `"ok":true`) {
		t.Errorf("body: %q", v.Response.Body.String)
	}
	if lastMethod != "POST" || lastPath != "/api/users" {
		t.Errorf("upstream saw %s %s", lastMethod, lastPath)
	}
	if lastBody != `{"name":"jassim"}` {
		t.Errorf("body forwarded: %q", lastBody)
	}
	// Custom header round-tripped.
	if v.Response.Headers["X-Upstream"] != "yes" {
		t.Errorf("missing X-Upstream in response: %v", v.Response.Headers)
	}
}

func TestProxyStripsHopByHopHeaders(t *testing.T) {
	var seenConnection string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenConnection = r.Header.Get("Connection")
	}))
	defer srv.Close()

	req := buildRequestObj("GET", "/", map[string]string{
		"Connection":    "should-be-dropped",
		"X-Custom":      "kept",
		"Authorization": "Bearer x",
	}, "")
	if _, err := builtinProxy(nil, []Value{StringValue(srv.URL), req}); err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if seenConnection != "" {
		t.Errorf("Connection header should be hop-by-hop stripped, got %q", seenConnection)
	}
}

func TestProxyAddsXForwardedFor(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Forwarded-For")
	}))
	defer srv.Close()

	req := buildRequestObj("GET", "/", nil, "")
	r := req.Object
	r.Set("ip", StringValue("203.0.113.7"))
	if _, err := builtinProxy(nil, []Value{StringValue(srv.URL), req}); err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if seen != "203.0.113.7" {
		t.Errorf("X-Forwarded-For: got %q", seen)
	}
}
