package interpreter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWSConnectRoundtripsAgainstServer runs both halves: an httptest
// server upgrades the request via the same upgradeWebSocket path
// `ws /chat` routes use, then DialWebSocket connects from the client
// side. A text frame should round-trip cleanly with masking applied
// outbound and stripped inbound.
func TestWSConnectRoundtripsAgainstServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgradeWebSocket(w, r)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		// Echo back the first message we receive, then close.
		text, _, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		conn.WriteText("echo:" + text)
		conn.WriteClose(1000, "")
	}))
	defer srv.Close()

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1)
	client, err := DialWebSocket(wsURL, nil)
	if err != nil {
		t.Fatalf("DialWebSocket: %v", err)
	}
	if !client.clientSide {
		t.Error("expected clientSide=true on outbound conn")
	}
	if err := client.WriteText("hello from client"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "echo:hello from client" {
		t.Errorf("got %q, want echo:hello from client", got)
	}
	client.WriteClose(1000, "bye")
}

func TestWSConnectRejectsHTTPScheme(t *testing.T) {
	_, err := DialWebSocket("https://example.com/ws", nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported scheme") {
		t.Errorf("expected scheme error, got %v", err)
	}
}

func TestWSConnectAcceptHeaderMatchesClientKey(t *testing.T) {
	// The accept-header derivation must produce exactly the value
	// from RFC 6455 §1.3 worked example: key = "dGhlIHNhbXBsZSBub25jZQ=="
	// → accept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	got := wsAcceptHeader("dGhlIHNhbXBsZSBub25jZQ==")
	want := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
