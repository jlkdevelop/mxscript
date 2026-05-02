// websocket.go — minimal RFC 6455 WebSocket server in pure stdlib. No
// extensions, no permessage-deflate, no fragmented messages over 16 MB.
// Good enough for chat / live updates / collaborative editors. If you
// need the heavyweight features, drop in gorilla/websocket and call
// Interpreter.Handler() to keep the rest of the framework.
package interpreter

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// websocketGUID is the magic constant defined by RFC 6455 §1.3 used to
// derive Sec-WebSocket-Accept from the client's Sec-WebSocket-Key.
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WSConn is the per-connection handle exposed to .mx code. Methods are
// safe for concurrent use; the underlying conn is guarded by writeMu.
type WSConn struct {
	conn    net.Conn
	rw      *bufio.ReadWriter
	writeMu sync.Mutex
	closed  bool
	closeMu sync.Mutex
}

// upgradeWebSocket performs the handshake on a regular HTTP request
// and returns the persistent WSConn the route can read/write.
func upgradeWebSocket(w http.ResponseWriter, r *http.Request) (*WSConn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("not a websocket upgrade request")
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return nil, errors.New("missing Connection: upgrade header")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key header")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("websocket: server doesn't support hijacking")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	accept := wsAcceptKey(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := rw.Write([]byte(resp)); err != nil {
		conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return &WSConn{conn: conn, rw: rw}, nil
}

func wsAcceptKey(clientKey string) string {
	h := sha1.New()
	h.Write([]byte(clientKey + websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ReadMessage reads one application-level message from the peer. It
// transparently handles continuation frames, ping/pong, and close
// frames. Returns the raw payload as a string and a boolean indicating
// whether the message is text (true) or binary (false). On normal close
// it returns io.EOF; on protocol error it returns a descriptive error.
func (c *WSConn) ReadMessage() (string, bool, error) {
	var payload []byte
	var firstOpcode byte

	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(c.rw, header); err != nil {
			return "", false, err
		}
		fin := header[0]&0x80 != 0
		opcode := header[0] & 0x0f
		masked := header[1]&0x80 != 0
		length := int64(header[1] & 0x7f)

		switch length {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(c.rw, ext); err != nil {
				return "", false, err
			}
			length = int64(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(c.rw, ext); err != nil {
				return "", false, err
			}
			length = int64(binary.BigEndian.Uint64(ext))
		}

		var maskKey [4]byte
		if masked {
			if _, err := io.ReadFull(c.rw, maskKey[:]); err != nil {
				return "", false, err
			}
		}
		// Hard cap at 16 MiB per message to prevent DoS.
		if length > 16<<20 {
			c.WriteClose(1009, "message too big")
			return "", false, errors.New("websocket: message too big")
		}
		frame := make([]byte, length)
		if length > 0 {
			if _, err := io.ReadFull(c.rw, frame); err != nil {
				return "", false, err
			}
		}
		if masked {
			for i := range frame {
				frame[i] ^= maskKey[i%4]
			}
		}

		switch opcode {
		case 0x0: // continuation
			payload = append(payload, frame...)
		case 0x1, 0x2: // text / binary
			payload = append(payload, frame...)
			firstOpcode = opcode
		case 0x8: // close
			code := uint16(1005)
			reason := ""
			if len(frame) >= 2 {
				code = binary.BigEndian.Uint16(frame[:2])
				if len(frame) > 2 {
					reason = string(frame[2:])
				}
			}
			_ = c.WriteClose(int(code), reason)
			return "", false, io.EOF
		case 0x9: // ping
			c.writeFrame(0xA, frame) // pong with same payload
			continue
		case 0xA: // pong — ignore
			continue
		default:
			return "", false, fmt.Errorf("websocket: unsupported opcode 0x%x", opcode)
		}

		if fin {
			return string(payload), firstOpcode == 0x1, nil
		}
	}
}

// WriteText sends a text frame containing the given UTF-8 string.
func (c *WSConn) WriteText(s string) error { return c.writeFrame(0x1, []byte(s)) }

// WriteBinary sends a binary frame.
func (c *WSConn) WriteBinary(b []byte) error { return c.writeFrame(0x2, b) }

// WriteClose sends a close frame with the given code and reason and then
// closes the underlying TCP connection. Subsequent calls are no-ops.
func (c *WSConn) WriteClose(code int, reason string) error {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return nil
	}
	c.closed = true
	c.closeMu.Unlock()

	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload[:2], uint16(code))
	copy(payload[2:], reason)
	_ = c.writeFrame(0x8, payload)
	return c.conn.Close()
}

// writeFrame builds a single FIN frame with the given opcode + payload.
// Server frames are NEVER masked (RFC 6455 §5.3).
func (c *WSConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	header := make([]byte, 0, 14)
	header = append(header, 0x80|opcode) // FIN + opcode

	switch n := len(payload); {
	case n < 126:
		header = append(header, byte(n))
	case n < 1<<16:
		header = append(header, 126, 0, 0)
		binary.BigEndian.PutUint16(header[len(header)-2:], uint16(n))
	default:
		header = append(header, 127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(header[len(header)-8:], uint64(n))
	}

	if _, err := c.rw.Write(header); err != nil {
		return err
	}
	if _, err := c.rw.Write(payload); err != nil {
		return err
	}
	return c.rw.Flush()
}
