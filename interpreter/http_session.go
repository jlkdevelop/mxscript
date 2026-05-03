// http_session.go — stateful HTTP client with a cookie jar. Useful
// for client SDKs that consume legacy APIs (form login → session
// cookie → subsequent requests share the auth state). The standard
// `fetch()` builtin is stateless; `http.session()` returns an object
// with methods that share a cookie jar and base configuration.
//
//	let s = http.session({ base_url: "https://api.example.com" })
//	s.post("/login", { email: "x", password: "y" })
//	let user = s.get("/me")        // cookies from /login auto-attach
//	s.close()                       // optional, clears the jar
package interpreter

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"strings"
	"sync"
	"time"
)

type httpSession struct {
	mu      sync.Mutex
	client  *http.Client
	baseURL string
	headers map[string]string
}

// http.session(opts?) — returns an object with get/post/put/delete/close.
// opts:
//
//	base_url:  prefix for all requests (e.g. "https://api.example.com")
//	headers:   default headers attached to every request
//	timeout:   per-request timeout in seconds (default 60)
func builtinHTTPSession(_ *Interpreter, args []Value) (Value, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return Value{}, err
	}
	timeout := 60 * time.Second
	baseURL := ""
	headers := map[string]string{}

	if len(args) > 0 && args[0].Kind == KindObject {
		opts := args[0].Object
		if v, ok := opts.Get("base_url"); ok && v.Kind == KindString {
			baseURL = strings.TrimRight(v.String, "/")
		}
		if v, ok := opts.Get("timeout"); ok && v.Kind == KindNumber {
			timeout = time.Duration(v.Number) * time.Second
		}
		if v, ok := opts.Get("headers"); ok && v.Kind == KindObject {
			for _, k := range v.Object.Keys {
				val, _ := v.Object.Get(k)
				headers[k] = val.Display()
			}
		}
	}
	s := &httpSession{
		client:  &http.Client{Jar: jar, Timeout: timeout},
		baseURL: baseURL,
		headers: headers,
	}

	out := NewOrderedMap()
	out.Set("get", FunctionValue(&Function{Name: "session.get", Native: func(_ *Interpreter, a []Value) (Value, error) {
		return s.do("GET", a)
	}}))
	out.Set("post", FunctionValue(&Function{Name: "session.post", Native: func(_ *Interpreter, a []Value) (Value, error) {
		return s.do("POST", a)
	}}))
	out.Set("put", FunctionValue(&Function{Name: "session.put", Native: func(_ *Interpreter, a []Value) (Value, error) {
		return s.do("PUT", a)
	}}))
	out.Set("delete", FunctionValue(&Function{Name: "session.delete", Native: func(_ *Interpreter, a []Value) (Value, error) {
		return s.do("DELETE", a)
	}}))
	out.Set("cookies", FunctionValue(&Function{Name: "session.cookies", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		return s.snapshotCookies(), nil
	}}))
	out.Set("close", FunctionValue(&Function{Name: "session.close", Native: func(_ *Interpreter, _ []Value) (Value, error) {
		// New jar replaces the old one — clears every stored cookie.
		j, _ := cookiejar.New(nil)
		s.mu.Lock()
		s.client.Jar = j
		s.mu.Unlock()
		return NullValue(), nil
	}}))
	return ObjectValue(out), nil
}

// do is the shared method that handles every verb. Args are
// (path_or_url) for GET/DELETE, (path_or_url, body?) for POST/PUT.
func (s *httpSession) do(method string, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("session.%s(url, body?) requires a url string", strings.ToLower(method))
	}
	target := args[0].String
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = s.baseURL + target
	}

	var body io.Reader
	contentType := ""
	if len(args) > 1 && (method == "POST" || method == "PUT") {
		switch args[1].Kind {
		case KindString:
			body = strings.NewReader(args[1].String)
			contentType = "application/x-www-form-urlencoded"
			// Heuristic: if it looks like JSON, switch the header.
			if strings.HasPrefix(strings.TrimSpace(args[1].String), "{") ||
				strings.HasPrefix(strings.TrimSpace(args[1].String), "[") {
				contentType = "application/json"
			}
		case KindObject, KindArray:
			raw, err := jsonEncode(args[1])
			if err != nil {
				return Value{}, err
			}
			body = bytes.NewReader(raw)
			contentType = "application/json"
		case KindNull:
			// no body
		default:
			body = strings.NewReader(args[1].Display())
			contentType = "text/plain"
		}
	}

	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return Value{}, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	s.mu.Lock()
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	s.mu.Unlock()

	resp, err := s.client.Do(req)
	if err != nil {
		return Value{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Value{}, err
	}

	out := NewOrderedMap()
	out.Set("status", NumberValue(float64(resp.StatusCode)))
	out.Set("text", StringValue(string(raw)))

	headers := NewOrderedMap()
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers.Set(k, StringValue(v[0]))
		}
	}
	out.Set("headers", ObjectValue(headers))

	// Convenience: when the response is JSON, parse it and expose .body.
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") && len(raw) > 0 {
		decoded, err := jsonDecode(raw)
		if err == nil {
			out.Set("body", decoded)
		}
	}
	return ObjectValue(out), nil
}

// snapshotCookies returns the cookie jar's contents for the session's
// base URL. Useful for debugging + persisting the state across runs
// (write to disk, hydrate on the next call).
func (s *httpSession) snapshotCookies() Value {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client.Jar == nil || s.baseURL == "" {
		return ArrayValue(nil)
	}
	u, err := neturl.Parse(s.baseURL)
	if err != nil {
		return ArrayValue(nil)
	}
	cookies := s.client.Jar.Cookies(u)
	out := make([]Value, len(cookies))
	for i, c := range cookies {
		om := NewOrderedMap()
		om.Set("name", StringValue(c.Name))
		om.Set("value", StringValue(c.Value))
		om.Set("path", StringValue(c.Path))
		om.Set("domain", StringValue(c.Domain))
		out[i] = ObjectValue(om)
	}
	return ArrayValue(out)
}
