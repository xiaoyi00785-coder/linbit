#!/usr/bin/env python3
"""See whether this host receives Binance USD-M websocket events, and whether
market-data has a live TCP connection to Binance."""
import base64, os, socket, ssl, struct, subprocess, time

def ip_port(spec):
    hexip, hexport = spec.split(":")
    b = bytes.fromhex(hexip)
    ip = ".".join(str(x) for x in b[::-1])
    return ip, int(hexport, 16)

pid = subprocess.check_output(
    ["docker", "inspect", "-f", "{{.State.Pid}}", "linbit-market-data-1"], text=True
).strip()
print("PID", pid)
path = f"/proc/{pid}/net/tcp"
try:
    lines = open(path).read().splitlines()[1:]
except OSError as e:
    print("net", e)
    lines = []
for line in lines:
    parts = line.split()
    st = parts[3]
    if st != "01":
        continue
    lip, lp = ip_port(parts[1])
    rip, rp = ip_port(parts[2])
    print("EST", f"{lip}:{lp}", "->", f"{rip}:{rp}")

def frames(s, deadline, limit=3):
    data = b""
    s.settimeout(0.5)
    n = 0
    while time.time() < deadline and n < limit:
        try:
            chunk = s.recv(65536)
        except socket.timeout:
            continue
        if not chunk:
            print("EOF")
            break
        data += chunk
        while len(data) >= 2 and n < limit:
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
            n += 1
            print("FRAME", n, "op", op, payload[:240])
    print("FRAMES", n)

def connect(path, host="fstream.binance.com"):
    raw = socket.create_connection((host, 443), 10)
    s = ssl.create_default_context().wrap_socket(raw, server_hostname=host)
    key = base64.b64encode(os.urandom(16)).decode()
    req = (
        f"GET {path} HTTP/1.1\r\nHost: {host}\r\nUpgrade: websocket\r\n"
        "Connection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
    )
    s.sendall(req.encode())
    buf = b""
    while b"\r\n\r\n" not in buf:
        buf += s.recv(4096)
    head = buf.split(b"\r\n", 1)[0]
    print("HANDSHAKE", path, head.decode(errors="replace"))
    return s

def send(s, obj):
    import json
    payload = json.dumps(obj).encode()
    mask = os.urandom(4)
    n = len(payload)
    hdr = bytes([0x81, 0x80 | n])
    masked = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
    s.sendall(hdr + mask + masked)

print("--- subscribe /ws ---")
try:
    s = connect("/ws")
    send(s, {"method": "SUBSCRIBE", "params": ["btcusdt@miniTicker"], "id": 1})
    frames(s, time.time() + 6, 3)
    s.close()
except Exception as e:
    print("SUB_ERR", type(e).__name__, e)

print("--- direct stream ---")
try:
    s = connect("/ws/btcusdt@miniTicker")
    frames(s, time.time() + 6, 2)
    s.close()
except Exception as e:
    print("DIRECT_ERR", type(e).__name__, e)

print("PROBE_DONE")
