package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Minimal server-side WebSocket (RFC 6455) over the standard library — enough to
// serve the public market-data channels without pulling in an external
// dependency (matching the house style, cf. the engine's hand-rolled HTTP).

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WebSocket opcodes.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// maxFrame caps a single inbound message payload (client requests are tiny).
const maxFrame = 1 << 20

// wsConn is a hijacked WebSocket connection.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
}

// isWebSocketUpgrade reports whether r is a WebSocket handshake request.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		r.Header.Get("Sec-WebSocket-Key") != ""
}

// upgradeWS performs the handshake and hijacks the connection.
func upgradeWS(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !isWebSocketUpgrade(r) {
		return nil, errors.New("not a websocket upgrade")
	}
	h, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("response writer does not support hijacking")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	sum := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])

	conn, brw, err := h.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := brw.WriteString(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if err := brw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return &wsConn{conn: conn, br: brw.Reader}, nil
}

// readFrame reads one frame, returning its opcode and (unmasked) payload. It
// transparently rejects fragmented control frames and enforces the size cap.
func (c *wsConn) readFrame() (opcode byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(c.br, hdr[:]); err != nil {
		return 0, nil, err
	}
	fin := hdr[0]&0x80 != 0
	opcode = hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	length := int(hdr[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint64(ext[:]))
	}
	if length < 0 || length > maxFrame {
		return 0, nil, errors.New("frame too large")
	}
	// Clients MUST mask; read the key if present.
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	if !fin && opcode != opContinuation {
		// Control frames are never fragmented; data-frame fragmentation is not
		// used by our clients. Treat the single frame as the whole message.
		return opcode, payload, nil
	}
	return opcode, payload, nil
}

// writeFrame writes a single unmasked server frame.
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	var hdr []byte
	b0 := byte(0x80) | opcode // FIN set
	n := len(payload)
	switch {
	case n <= 125:
		hdr = []byte{b0, byte(n)}
	case n <= 0xFFFF:
		hdr = []byte{b0, 126, 0, 0}
		binary.BigEndian.PutUint16(hdr[2:], uint16(n))
	default:
		hdr = make([]byte, 10)
		hdr[0], hdr[1] = b0, 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
	}
	if _, err := c.conn.Write(hdr); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

func (c *wsConn) writeText(b []byte) error { return c.writeFrame(opText, b) }
func (c *wsConn) writeClose() error        { return c.writeFrame(opClose, nil) }
func (c *wsConn) Close() error             { return c.conn.Close() }

func (c *wsConn) setReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }
