#!/usr/bin/env python3
"""Count public market frames for 6 seconds."""
import base64, json, os, socket, ssl, struct, time

def connect():
    host = "api.cowa.click"
    raw = socket.create_connection((host, 443), 8)
    s = ssl.create_default_context().wrap_socket(raw, server_hostname=host)
    key = base64.b64encode(os.urandom(16)).decode()
    s.sendall((
        "GET /ws HTTP/1.1\r\nHost: api.cowa.click\r\nUpgrade: websocket\r\n"
        "Connection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
    ).encode())
    buf = b""
    s.settimeout(8)
    while b"\r\n\r\n" not in buf:
        buf += s.recv(4096)
    print(buf.split(b"\r\n", 1)[0].decode(errors="replace"))
    return s

def send(s, obj):
    payload = json.dumps(obj).encode()
    mask = os.urandom(4)
    n = len(payload)
    if n < 126:
        hdr = bytes([0x81, 0x80 | n])
    else:
        hdr = bytes([0x81, 0xFE]) + struct.pack("!H", n)
    s.sendall(hdr + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(payload)))

s = connect()
send(s, {"op": "subscribe", "args": [
    "futures-ticker:BTCUSDT", "futures-depth:BTCUSDT", "futures-trades:BTCUSDT", "futures-kline:BTCUSDT:1m",
    "ticker:BTCUSDT", "depth:BTCUSDT", "trades:BTCUSDT", "kline:BTCUSDT:1m",
]})
counts = {}
data = b""
s.settimeout(0.4)
end = time.time() + 6
while time.time() < end:
    try:
        chunk = s.recv(65536)
    except socket.timeout:
        continue
    if not chunk:
        break
    data += chunk
    while len(data) >= 2:
        op = data[0] & 0x0F
        ln = data[1] & 0x7F
        i = 2
        if ln == 126:
            if len(data) < 4:
                break
            ln = struct.unpack("!H", data[2:4])[0]
            i = 4
        elif ln == 127:
            if len(data) < 10:
                break
            ln = struct.unpack("!Q", data[2:10])[0]
            i = 10
        if len(data) < i + ln:
            break
        payload = data[i:i + ln]
        data = data[i + ln:]
        if op != 1:
            continue
        try:
            msg = json.loads(payload)
        except Exception:
            continue
        ch = msg.get("channel") or msg.get("event") or "?"
        kind = ch.split(":")[0]
        counts[kind] = counts.get(kind, 0) + 1
print("COUNTS", json.dumps(counts, sort_keys=True))
print("PROBE_CH_DONE")
