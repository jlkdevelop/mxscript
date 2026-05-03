package interpreter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// httpSessionMockServer simulates a tiny login-gated API. POST /login
// sets a session cookie; GET /me reads the cookie and returns the
// stored value. Lets us verify the cookie jar works.
func httpSessionMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc123", Path: "/"})
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"no session"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"session":"` + c.Value + `"}`))
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("X-Auth")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"x_auth":"` + auth + `"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPSessionPersistsCookies(t *testing.T) {
	srv := httpSessionMockServer(t)
	opts := NewOrderedMap()
	opts.Set("base_url", StringValue(srv.URL))

	sessVal, err := builtinHTTPSession(nil, []Value{ObjectValue(opts)})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	get, _ := sessVal.Object.Get("get")
	post, _ := sessVal.Object.Get("post")

	// POST /login — server sends session cookie.
	if _, err := post.Function.Native(nil, []Value{StringValue("/login"), NullValue()}); err != nil {
		t.Fatalf("login: %v", err)
	}
	// GET /me — should auto-attach the cookie.
	v, err := get.Function.Native(nil, []Value{StringValue("/me")})
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	status, _ := v.Object.Get("status")
	if status.Number != 200 {
		t.Errorf("expected 200, got %v", status.Number)
	}
	body, _ := v.Object.Get("body")
	sessVal2, _ := body.Object.Get("session")
	if sessVal2.String != "abc123" {
		t.Errorf("session cookie didn't round-trip; got %v", sessVal2)
	}
}

func TestHTTPSessionAttachesDefaultHeaders(t *testing.T) {
	srv := httpSessionMockServer(t)
	hdrs := NewOrderedMap()
	hdrs.Set("X-Auth", StringValue("Bearer token-abc"))
	opts := NewOrderedMap()
	opts.Set("base_url", StringValue(srv.URL))
	opts.Set("headers", ObjectValue(hdrs))

	sessVal, _ := builtinHTTPSession(nil, []Value{ObjectValue(opts)})
	get, _ := sessVal.Object.Get("get")
	v, _ := get.Function.Native(nil, []Value{StringValue("/echo")})
	body, _ := v.Object.Get("body")
	xAuth, _ := body.Object.Get("x_auth")
	if xAuth.String != "Bearer token-abc" {
		t.Errorf("default header not attached: got %v", xAuth)
	}
}

func TestHTTPSessionPostJSONBody(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, r.ContentLength)
		r.Body.Read(raw)
		receivedBody = string(raw)
		w.Header().Set("Content-Type", r.Header.Get("Content-Type"))
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	opts := NewOrderedMap()
	opts.Set("base_url", StringValue(srv.URL))
	sessVal, _ := builtinHTTPSession(nil, []Value{ObjectValue(opts)})
	post, _ := sessVal.Object.Get("post")

	payload := NewOrderedMap()
	payload.Set("email", StringValue("a@b.c"))
	payload.Set("count", NumberValue(3))
	if _, err := post.Function.Native(nil, []Value{StringValue("/"), ObjectValue(payload)}); err != nil {
		t.Fatalf("post: %v", err)
	}
	if !strings.Contains(receivedBody, `"email":"a@b.c"`) {
		t.Errorf("expected JSON body, got %q", receivedBody)
	}
	if !strings.Contains(receivedBody, `"count":3`) {
		t.Errorf("expected count in body, got %q", receivedBody)
	}
}

func TestHTTPSessionCloseClearsCookies(t *testing.T) {
	srv := httpSessionMockServer(t)
	opts := NewOrderedMap()
	opts.Set("base_url", StringValue(srv.URL))
	sessVal, _ := builtinHTTPSession(nil, []Value{ObjectValue(opts)})
	post, _ := sessVal.Object.Get("post")
	get, _ := sessVal.Object.Get("get")
	closeFn, _ := sessVal.Object.Get("close")

	post.Function.Native(nil, []Value{StringValue("/login"), NullValue()})
	closeFn.Function.Native(nil, nil)
	v, _ := get.Function.Native(nil, []Value{StringValue("/me")})
	status, _ := v.Object.Get("status")
	if status.Number != 401 {
		t.Errorf("after close, /me should 401; got %v", status.Number)
	}
}
