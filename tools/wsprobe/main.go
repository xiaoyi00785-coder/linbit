// Command wsprobe is a tiny, dependency-free WebSocket client used by
// scripts/smoke-market.sh to verify the market-data push pipeline end to end: it
// connects (optionally through the gateway), subscribes to a set of channels, and
// exits 0 once it has received a data frame for every required channel (or 1 on
// timeout). It speaks just enough of RFC 6455 for text frames.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	target := flag.String("url", "ws://localhost:8080/ws", "WebSocket URL")
	channelsCSV := flag.String("channels", "ticker:BTCUSDT,depth:BTCUSDT,kline:BTCUSDT:1m,trades:BTCUSDT", "required channels (CSV)")
	timeout := flag.Int("timeout", 15, "seconds to wait for all channels")
	flag.Parse()

	required := map[string]bool{}
	for _, c := range strings.Split(*channelsCSV, ",") {
		if c = strings.TrimSpace(c); c != "" {
			required[c] = false
		}
	}

	conn, err := dialWS(*target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wsprobe: connect: %v\n", err)
		os.Exit(2)
	}
	defer conn.Close()

	// Subscribe to every required channel.
	args := make([]string, 0, len(required))
	for c := range required {
		args = append(args, c)
	}
	sub, _ := json.Marshal(map[string]any{"id": "1", "op": "subscribe", "args": args})
	if err := conn.writeText(sub); err != nil {
		fmt.Fprintf(os.Stderr, "wsprobe: subscribe: %v\n", err)
		os.Exit(2)
	}

	deadline := time.Now().Add(time.Duration(*timeout) * time.Second)
	_ = conn.raw.SetReadDeadline(deadline)

	for {
		op, payload, err := conn.readFrame()
		if err != nil {
			fmt.Fprintf(os.Stderr, "wsprobe: read: %v\n", err)
			reportMissing(required)
			os.Exit(1)
		}
		switch op {
		case opText:
			handleText(conn, payload, required)
			if allSeen(required) {
				fmt.Println("wsprobe: all required channels received")
				os.Exit(0)
			}
		case opPing:
			_ = conn.writeFrame(opPong, payload)
		case opClose:
			reportMissing(required)
			os.Exit(1)
		}
	}
}

func handleText(conn *wsConn, payload []byte, required map[string]bool) {
	var msg struct {
		Channel string `json:"channel"`
		Event   string `json:"event"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return
	}
	// App-level heartbeat per docs/api/websocket.md.
	if msg.Event == "ping" {
		pong, _ := json.Marshal(map[string]any{"op": "pong"})
		_ = conn.writeText(pong)
		return
	}
	if msg.Channel == "" {
		return
	}
	if _, ok := required[msg.Channel]; ok && !required[msg.Channel] {
		required[msg.Channel] = true
		fmt.Printf("wsprobe: SEEN %s\n", msg.Channel)
	}
}

func allSeen(required map[string]bool) bool {
	for _, seen := range required {
		if !seen {
			return false
		}
	}
	return true
}

func reportMissing(required map[string]bool) {
	for ch, seen := range required {
		if !seen {
			fmt.Fprintf(os.Stderr, "wsprobe: MISSING %s\n", ch)
		}
	}
}

// ---- minimal WebSocket client ----

const (
	opText  = 0x1
	opClose = 0x8
	opPing  = 0x9
	opPong  = 0xA
)

type wsConn struct {
	raw net.Conn
	br  *bufio.Reader
}

func dialWS(target string) (*wsConn, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if u.Port() == "" {
		host += ":80"
	}
	raw, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	_, _ = rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := raw.Write([]byte(req)); err != nil {
		raw.Close()
		return nil, err
	}
	br := bufio.NewReader(raw)
	status, err := br.ReadString('\n')
	if err != nil {
		raw.Close()
		return nil, err
	}
	if !strings.Contains(status, "101") {
		raw.Close()
		return nil, fmt.Errorf("unexpected handshake status: %s", strings.TrimSpace(status))
	}
	// Drain the rest of the response headers.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			raw.Close()
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	return &wsConn{raw: raw, br: br}, nil
}

func (c *wsConn) readFrame() (byte, []byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
		return 0, nil, err
	}
	opcode := hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	length := int(hdr[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint64(ext[:]))
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

// writeFrame writes a masked client frame (clients MUST mask).
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	var hdr []byte
	b0 := byte(0x80) | opcode
	n := len(payload)
	switch {
	case n <= 125:
		hdr = []byte{b0, byte(0x80 | n)}
	case n <= 0xFFFF:
		hdr = []byte{b0, 0x80 | 126, 0, 0}
		binary.BigEndian.PutUint16(hdr[2:], uint16(n))
	default:
		hdr = make([]byte, 10)
		hdr[0], hdr[1] = b0, 0x80|127
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
	}
	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}
	buf := append(append(hdr, mask[:]...), masked...)
	_, err := c.raw.Write(buf)
	return err
}

func (c *wsConn) writeText(b []byte) error { return c.writeFrame(opText, b) }
func (c *wsConn) Close() error             { return c.raw.Close() }
