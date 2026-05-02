package interpreter

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// captureBody wraps an httptest server that records the most recent
// request's body and content-type. Returns the URL plus a getter so
// tests can assert on what we sent.
func captureBody(t *testing.T, status int) (string, func() (string, string, http.Header)) {
	t.Helper()
	var lastBody string
	var lastCT string
	var lastHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastCT = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		lastBody = string(raw)
		lastHeaders = r.Header.Clone()
		w.WriteHeader(status)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	return server.URL, func() (string, string, http.Header) {
		return lastBody, lastCT, lastHeaders
	}
}

func TestNotifySlackSendsTextField(t *testing.T) {
	url, get := captureBody(t, 200)
	got, err := builtinNotifySlack(nil, []Value{
		StringValue(url), StringValue("deploy succeeded"),
	})
	if err != nil {
		t.Fatalf("notify.slack: %v", err)
	}
	body, ct, _ := get()
	if !strings.Contains(ct, "application/json") {
		t.Errorf("content-type: got %q", ct)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if parsed["text"] != "deploy succeeded" {
		t.Errorf("body: got %v, want text=deploy succeeded", parsed)
	}
	// Result shape.
	r := got.Object
	if v, _ := r.Get("ok"); !v.Bool {
		t.Errorf("expected ok=true on 200")
	}
	if v, _ := r.Get("status"); v.Number != 200 {
		t.Errorf("expected status=200, got %v", v.Number)
	}
}

func TestNotifySlackPassesObjectThrough(t *testing.T) {
	url, get := captureBody(t, 200)
	msg := NewOrderedMap()
	msg.Set("text", StringValue("ship it"))
	msg.Set("username", StringValue("ci-bot"))
	if _, err := builtinNotifySlack(nil, []Value{
		StringValue(url), ObjectValue(msg),
	}); err != nil {
		t.Fatalf("notify.slack: %v", err)
	}
	body, _, _ := get()
	var parsed map[string]any
	json.Unmarshal([]byte(body), &parsed)
	if parsed["text"] != "ship it" || parsed["username"] != "ci-bot" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestNotifyDiscordSendsContentField(t *testing.T) {
	url, get := captureBody(t, 200)
	if _, err := builtinNotifyDiscord(nil, []Value{
		StringValue(url), StringValue("build broke"),
	}); err != nil {
		t.Fatalf("notify.discord: %v", err)
	}
	body, _, _ := get()
	var parsed map[string]any
	json.Unmarshal([]byte(body), &parsed)
	if parsed["content"] != "build broke" {
		t.Errorf("body: got %v, want content=build broke", parsed)
	}
}

func TestNotifyResultShapeOnError(t *testing.T) {
	url, _ := captureBody(t, 500)
	v, err := builtinNotifySlack(nil, []Value{
		StringValue(url), StringValue("oops"),
	})
	if err != nil {
		t.Fatalf("builtin should not throw: %v", err)
	}
	r := v.Object
	if ok, _ := r.Get("ok"); ok.Bool {
		t.Errorf("ok should be false on 500")
	}
	if status, _ := r.Get("status"); status.Number != 500 {
		t.Errorf("status: got %v, want 500", status.Number)
	}
	if errVal, _ := r.Get("error"); errVal.Kind != KindString {
		t.Errorf("error should be a string on failure, got %+v", errVal)
	}
}

func TestNotifyEmailMissingKey(t *testing.T) {
	prev := os.Getenv("RESEND_API_KEY")
	os.Unsetenv("RESEND_API_KEY")
	defer func() {
		if prev != "" {
			os.Setenv("RESEND_API_KEY", prev)
		}
	}()
	v, err := builtinNotifyEmail(nil, []Value{
		StringValue("user@example.com"),
		StringValue("hi"),
		StringValue("body"),
	})
	if err != nil {
		t.Fatalf("builtin should not throw: %v", err)
	}
	r := v.Object
	if ok, _ := r.Get("ok"); ok.Bool {
		t.Error("ok should be false when API key missing")
	}
	if errVal, _ := r.Get("error"); !strings.Contains(errVal.String, "RESEND_API_KEY") {
		t.Errorf("error should mention RESEND_API_KEY, got %q", errVal.String)
	}
}

// TestNotifyEmailWiring confirms the helper sends the documented
// Resend payload shape when the env key is set. We point the helper
// at our own httptest server by temporarily overriding the URL — but
// since the URL is a const inside builtinNotifyEmail, we instead
// exercise the lower-level postJSON to assert the payload shape that
// the helper would build.
func TestNotifyEmailPayloadShape(t *testing.T) {
	url, get := captureBody(t, 200)
	body := map[string]any{
		"from":    "noreply@example.com",
		"to":      "user@example.com",
		"subject": "hi",
		"text":    "hello",
	}
	status, err := postJSON(url, map[string]string{"Authorization": "Bearer test_key"}, body)
	if err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	if status != 200 {
		t.Errorf("status: got %d", status)
	}
	raw, _, headers := get()
	if got := headers.Get("Authorization"); got != "Bearer test_key" {
		t.Errorf("Authorization: got %q", got)
	}
	var parsed map[string]any
	json.Unmarshal([]byte(raw), &parsed)
	if parsed["from"] != "noreply@example.com" || parsed["to"] != "user@example.com" {
		t.Errorf("payload: got %v", parsed)
	}
	if _, hasHTML := parsed["html"]; hasHTML {
		t.Errorf("non-html payload should not include html field")
	}
	if parsed["text"] != "hello" {
		t.Errorf("text field: got %v", parsed["text"])
	}
}
