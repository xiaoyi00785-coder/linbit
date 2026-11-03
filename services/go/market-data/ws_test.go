package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net"
	"testing"
	"time"
)

// nopConn adapts an io.Writer to net.Conn for write-path tests.
type nopConn struct{ w io.Writer }

func (c nopConn) Write(b []byte) (int, error)      { return c.w.Write(b) }
func (c nopConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c nopConn) Close() error                     { return nil }
func (c nopConn) LocalAddr() net.Addr              { return nil }
func (c nopConn) RemoteAddr() net.Addr             { return nil }
func (c nopConn) SetDeadline(time.Time) error      { return nil }
func (c nopConn) SetReadDeadline(time.Time) error  { return nil }
func (c nopConn) SetWriteDeadline(time.Time) error { return nil }

// TestAcceptKey checks the handshake accept computation against the RFC 6455
// worked example.
func TestAcceptKey(t *testing.T) {
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	sum := sha1.Sum([]byte(key + wsGUID))
	got := base64.StdEncoding.EncodeToString(sum[:])
	want := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got != want {
		t.Fatalf("accept = %q, want %q", got, want)
	}
}

// maskedTextFrame builds a client text frame (FIN set, masked) for the decoder.
func maskedTextFrame(payload string) []byte {
	mask := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	b := []byte(payload)
	out := []byte{0x80 | opText, byte(0x80 | len(b))}
	out = append(out, mask[:]...)
	for i := range b {
		out = append(out, b[i]^mask[i%4])
	}
	return out
}

func TestReadFrameUnmasksClientPayload(t *testing.T) {
	raw := maskedTextFrame(`{"op":"ping"}`)
	c := &wsConn{br: bufio.NewReader(bytes.NewReader(raw))}
	op, payload, err := c.readFrame()
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if op != opText {
		t.Fatalf("opcode = %d", op)
	}
	if string(payload) != `{"op":"ping"}` {
		t.Fatalf("payload = %q", payload)
	}
}

func TestWriteFrameServerUnmasked(t *testing.T) {
	var buf bytes.Buffer
	c := &wsConn{conn: nopConn{&buf}}
	if err := c.writeText([]byte("hi")); err != nil {
		t.Fatalf("writeText: %v", err)
	}
	got := buf.Bytes()
	// FIN + text, len 2, no mask bit, then "hi".
	want := []byte{0x81, 0x02, 'h', 'i'}
	if !bytes.Equal(got, want) {
		t.Fatalf("frame = % x, want % x", got, want)
	}
}
