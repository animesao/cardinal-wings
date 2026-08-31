// Package ws implements the smallest practical RFC 6455 websocket server on
// the standard library: the HTTP upgrade handshake plus text frames. It is
// intentionally minimal — enough for a panel terminal — and dependency-free.
package ws

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
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Conn is a websocket connection.
type Conn struct {
	conn      net.Conn
	r         *bufio.Reader
	w         *bufio.Writer
	writeMu   sync.Mutex
	closeOnce sync.Once
}

// Upgrade performs the websocket handshake on an HTTP request and hijacks the
// connection. The caller must have already validated the request (auth, path).
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("not a websocket upgrade request")
	}
	if !headerContainsToken(r.Header.Get("Connection"), "upgrade") {
		return nil, errors.New("missing connection: upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("hijacking unsupported")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	// Compute accept key: SHA1(key + GUID) base64.
	h := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(h[:])

	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if brw != nil {
		if err := brw.Flush(); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	// Hijack returns a bufio.ReadWriter that may already contain the first
	// websocket frame when the browser sends it in the same TCP packet as the
	// HTTP upgrade. Replacing brw.Reader here loses those bytes and leaves the
	// frame parser out of sync, which commonly surfaces as ECONNRESET in the
	// panel. Keep both buffered sides returned by Hijack.
	return &Conn{conn: conn, r: brw.Reader, w: brw.Writer}, nil
}

// ReadText reads one text (or binary, treated as text) message, returning
// its payload. Control frames are handled internally so ping/pong bytes never
// reach the container stdin. Fragmented data messages are reassembled up to
// the same 4 MiB limit as a single frame. The connection must be read by one
// goroutine.
func (c *Conn) ReadText() ([]byte, error) {
	var message []byte
	var messageOpcode byte
	for {
		fin, opcode, payload, err := readFrame(c.r)
		if err != nil {
			return nil, err
		}
		switch opcode {
		case 0x8: // close
			// A close reply is required by RFC 6455. Echo the close payload,
			// then let the caller tear down the socket.
			if err := c.writeControl(0x8, payload); err != nil {
				return nil, err
			}
			return nil, io.EOF
		case 0x9: // ping -> pong
			if err := c.writeControl(0xA, payload); err != nil {
				return nil, err
			}
			continue
		case 0xA: // pong
			continue
		case 0x1, 0x2: // first text/binary data frame
			if messageOpcode != 0 {
				return nil, errors.New("new data frame before fragmented message ended")
			}
			messageOpcode = opcode
		case 0x0: // continuation frame
			if messageOpcode == 0 {
				return nil, errors.New("unexpected continuation frame")
			}
		default:
			return nil, fmt.Errorf("unexpected opcode %d", opcode)
		}

		if len(message)+len(payload) > 4<<20 {
			return nil, errors.New("message too large")
		}
		message = append(message, payload...)
		if fin {
			return message, nil
		}
		// A non-final text/binary frame must be followed by continuation
		// frames; control frames may appear between them and are handled above.
	}
}

// WriteText sends one text message. Writes are serialized because output,
// errors and close notifications may originate from different goroutines.
func (c *Conn) WriteText(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := writeFrame(c.w, 0x1, payload); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *Conn) writeControl(opcode byte, payload []byte) error {
	if len(payload) > 125 {
		return errors.New("control frame too large")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := writeFrame(c.w, opcode, payload); err != nil {
		return err
	}
	return c.w.Flush()
}

// Close sends a close frame and closes the socket exactly once.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.writeMu.Lock()
		_ = writeFrame(c.w, 0x8, nil)
		_ = c.w.Flush()
		c.writeMu.Unlock()
		err = c.conn.Close()
	})
	return err
}

func headerContainsToken(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func readFrame(r *bufio.Reader) (fin bool, opcode byte, payload []byte, err error) {
	var b [2]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		return
	}
	fin = b[0]&0x80 != 0
	rsv := b[0] & 0x70
	opcode = b[0] & 0x0f
	if rsv != 0 {
		return false, 0, nil, errors.New("unsupported websocket extensions")
	}
	masked := b[1]&0x80 != 0
	if !masked {
		return false, 0, nil, errors.New("client frame is not masked")
	}
	length := uint64(b[1] & 0x7f)

	switch {
	case length == 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case length == 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > 4<<20 {
		return false, 0, nil, errors.New("frame too large")
	}
	if opcode >= 0x8 && (!fin || length > 125) {
		return false, 0, nil, errors.New("invalid control frame")
	}

	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(r, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(r, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return
}

func writeFrame(w *bufio.Writer, opcode byte, payload []byte) error {
	var hdr [10]byte
	hdr[0] = 0x80 | opcode // FIN + opcode
	n := len(payload)
	idx := 2
	switch {
	case n <= 125:
		hdr[1] = byte(n)
	case n <= 0xFFFF:
		hdr[1] = 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(n))
		idx = 4
	default:
		hdr[1] = 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(n))
		idx = 10
	}
	if _, err := w.Write(hdr[:idx]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// SetReadDeadline is exposed for heartbeat loops.
func (c *Conn) SetReadDeadline(d time.Time) error { return c.conn.SetReadDeadline(d) }
