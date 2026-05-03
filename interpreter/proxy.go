// proxy.go — server-side reverse-proxy helper. Lets MX route
// handlers forward an incoming request to an upstream service.
// Useful for dev workflows where MX is the backend serving the API
// and a Vite / Next dev server is the frontend at a different port:
//
//	get /api/*  { return json(handle_api(...)) }
//	get /*      { return proxy("http://localhost:5173", request) }
//
// Streams body bytes through verbatim. Per-hop headers (Connection,
// Hop-by-Hop) are stripped per RFC 2616 §13.5.1.
package interpreter

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

// proxy(target_url, request) — issue the same HTTP method + path +
// body to `target_url` and return the upstream response wrapped in
// MX's Response shape so the caller can `return proxy(...)`.
//
// `request` is the route's `request` object — has `path`, `method`,
// `query`, `headers`, `body_text`. We reuse those fields so users
// don't have to thread the raw http.Request anywhere.
func builtinProxy(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 2 || args[0].Kind != KindString || args[1].Kind != KindObject {
		return Value{}, fmt.Errorf("proxy(target_url, request) requires (string, object)")
	}
	target := strings.TrimRight(args[0].String, "/")
	req := args[1].Object

	// Reconstruct path + query.
	pathV, _ := req.Get("path")
	path := "/"
	if pathV.Kind == KindString {
		path = pathV.String
	}
	queryStr := ""
	if q, _ := req.Get("query"); q.Kind == KindObject && len(q.Object.Keys) > 0 {
		params := neturl.Values{}
		for _, k := range q.Object.Keys {
			v, _ := q.Object.Get(k)
			params.Set(k, v.Display())
		}
		queryStr = "?" + params.Encode()
	}
	upstream := target + path + queryStr

	// Method + body.
	method := "GET"
	if m, _ := req.Get("method"); m.Kind == KindString {
		method = m.String
	}
	var body io.Reader
	if b, _ := req.Get("body_text"); b.Kind == KindString && b.String != "" {
		body = bytes.NewReader([]byte(b.String))
	}

	outReq, err := http.NewRequest(method, upstream, body)
	if err != nil {
		return Value{}, err
	}

	// Copy headers, dropping hop-by-hop ones (RFC 2616 §13.5.1).
	hopByHop := map[string]bool{
		"connection": true, "proxy-connection": true,
		"keep-alive": true, "proxy-authenticate": true,
		"proxy-authorization": true, "te": true,
		"trailers": true, "transfer-encoding": true,
		"upgrade": true,
	}
	if h, _ := req.Get("headers"); h.Kind == KindObject {
		for _, k := range h.Object.Keys {
			if hopByHop[strings.ToLower(k)] {
				continue
			}
			v, _ := h.Object.Get(k)
			outReq.Header.Set(k, v.Display())
		}
	}
	// Set X-Forwarded-For so upstream sees the original client.
	if ip, _ := req.Get("ip"); ip.Kind == KindString && ip.String != "" {
		outReq.Header.Set("X-Forwarded-For", ip.String)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(outReq)
	if err != nil {
		return Value{}, fmt.Errorf("proxy: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Value{}, fmt.Errorf("proxy: read body: %w", err)
	}

	// Build the Response. Copy status + Content-Type + a curated set
	// of headers (skip hop-by-hop on the way back too).
	respHeaders := map[string]string{}
	for k, v := range resp.Header {
		if hopByHop[strings.ToLower(k)] {
			continue
		}
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return ResponseValue(&Response{
		Status:      resp.StatusCode,
		ContentType: contentType,
		Body:        StringValue(string(raw)),
		Headers:     respHeaders,
	}), nil
}
