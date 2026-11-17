#!/usr/bin/env python3
import base64, json, os, socket, ssl, struct, time, urllib.request

def get(url):
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "linbit-probe"})
        with urllib.request.urlopen(req, timeout=8) as r:
            body = r.read(180).decode(errors="replace")
            print("REST", r.status, url.split("?")[0], body.replace("\n", " ")[:160])
    except Exception as e:
        print("REST_ERR", url.split("?")[0], type(e).__name__, e)

get("https://fapi.binance.com/fapi/v1/ticker/24hr?symbol=BTCUSDT")
get("https://api.binance.com/api/v3/ticker/24hr?symbol=BTCUSDT")
get("https://www.okx.com/api/v5/market/ticker?instId=BTC-USDT-SWAP")
get("https://api.bybit.com/v5/market/tickers?category=linear&symbol=BTCUSDT")

def frames(s, deadline, limit=2):
    data = b""
    s.settimeout(0.4)
    n = 0
    while time.time() < deadline and n < limit:
        try:
            chunk = s.recv(65536)
        except socket.timeout:
            continue
        if not chunk:
            print(" EOF")
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
            if data[1] & 0x80:
                print(" masked from server")
                return
            if len(data) < i + ln:
                break
            payload = data[i:i + ln]
            data = data[i + ln:]
            n += 1
            print(" FRAME", n, "op", op, payload[:220])
    print(" FRAMES", n)

def connect(host, path, port=443):
    raw = socket.create_connection((host, port), 8)
    s = ssl.create_default_context().wrap_socket(raw, server_hostname=host)
    key = base64.b64encode(os.urandom(16)).decode()
    s.sendall((
        f"GET {path} HTTP/1.1\r\nHost: {host}\r\nUpgrade: websocket\r\n"
        "Connection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
    ).encode())
    buf = b""
    s.settimeout(8)
    while b"\r\n\r\n" not in buf:
        buf += s.recv(4096)
    print("HANDSHAKE", host, path, buf.split(b"\r\n", 1)[0].decode(errors="replace"))
    rest = buf.split(b"\r\n\r\n", 1)[1]
    return s, rest

def send(s, obj):
    payload = json.dumps(obj).encode()
    mask = os.urandom(4)
    n = len(payload)
    hdr = bytes([0x81, 0x80 | n]) if n < 126 else bytes([0x81, 0xFE]) + struct.pack("!H", n)
    s.sendall(hdr + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(payload)))

tests = [
    ("fstream.binance.com", "/stream?streams=btcusdt@miniTicker", None),
    ("fstream.binance.com", "/ws/btcusdt@aggTrade", None),
    ("data-stream.binance.vision", "/ws/btcusdt@miniTicker", None),
    ("stream.binance.com", "/ws/btcusdt@miniTicker", None),
    ("ws.okx.com", "/ws/v5/public", {"op": "subscribe", "args": [{"channel": "tickers", "instId": "BTC-USDT-SWAP"}]}),
    ("stream.bybit.com", "/v5/public/linear", {"op": "subscribe", "args": ["tickers.BTCUSDT"]}),
]
for host, path, sub in tests:
    print("---", host, path)
    try:
        s, rest = connect(host, path)
        if rest:
            print(" PRE", rest[:120])
        if sub:
            send(s, sub)
        frames(s, time.time() + 5, 2)
        s.close()
    except Exception as e:
        print(" ERR", type(e).__name__, e)

print("PROBE2_DONE")
