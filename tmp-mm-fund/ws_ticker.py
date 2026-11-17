#!/usr/bin/env python3
import base64, json, os, socket, struct, subprocess, time

ip = subprocess.check_output(["docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", "linbit-market-data-1"], text=True).split()[0]
s = socket.create_connection((ip, 8085), 5)
key = base64.b64encode(os.urandom(16)).decode()
s.sendall((
    "GET /ws HTTP/1.1\r\nHost: md\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
    f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
).encode())
buf = b""
while b"\r\n\r\n" not in buf:
    buf += s.recv(4096)

def send(obj):
    payload = json.dumps(obj).encode()
    mask = os.urandom(4)
    n = len(payload)
    hdr = bytes([0x81, 0x80 | n]) if n < 126 else bytes([0x81, 0x80 | 126]) + struct.pack("!H", n)
    masked = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
    s.sendall(hdr + mask + masked)

def frames(deadline):
    data = b""
    s.settimeout(0.5)
    while time.time() < deadline:
        try:
            chunk = s.recv(65536)
        except socket.timeout:
            continue
        if not chunk:
            break
        data += chunk
        while len(data) >= 2:
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
            yield data[i:i + ln]
            data = data[i + ln:]

send({"op": "subscribe", "args": ["futures-ticker:BTCUSDT"]})
n = 0
for raw in frames(time.time() + 4):
    try:
        msg = json.loads(raw)
    except Exception:
        continue
    d = msg.get("data")
    if not isinstance(d, dict):
        continue
    if msg.get("channel") != "futures-ticker:BTCUSDT":
        continue
    n += 1
    print("MSG", n, "keys", sorted(d.keys()), "src", d.get("priceSource"), "last", d.get("lastPrice"), "turn", d.get("turnover24h") or d.get("quoteVolume"))
    if n >= 6:
        break
print("WS_TICK_DONE", n)
