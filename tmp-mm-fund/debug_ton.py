#!/usr/bin/env python3
import json, subprocess, urllib.request

def get(url):
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "linbit-mm/1.0", "Accept": "application/json"})
        with urllib.request.urlopen(req, timeout=15) as r:
            return r.status, r.read()[:500].decode()
    except Exception as e:
        return 0, str(e)

print("BYBIT_LIN", get("https://api.bybit.com/v5/market/orderbook?category=linear&symbol=TONUSDT&limit=5"))
print("BYBIT_SPOT", get("https://api.bybit.com/v5/market/orderbook?category=spot&symbol=TONUSDT&limit=5"))
print("OKX_SPOT", get("https://www.okx.com/api/v5/market/books?instId=TON-USDT&sz=5"))
print("OKX_SWAP", get("https://www.okx.com/api/v5/market/books?instId=TON-USDT-SWAP&sz=5"))

print("--- docker logs ---")
print(subprocess.check_output(["docker", "logs", "--tail", "40", "linbit-marketmaker-1"], text=True, stderr=subprocess.STDOUT)[-3000:])

tok = None
for line in open("/opt/linbit/.env"):
    if line.startswith("INTERNAL_TOKEN="):
        tok = line.split("=", 1)[1].strip().strip("'\"")
ip = subprocess.check_output(
    ["docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", "linbit-marketmaker-1"],
    text=True,
).split()[0]
req = urllib.request.Request("http://%s:8093/internal/mm/status" % ip, headers={"X-Internal-Token": tok})
with urllib.request.urlopen(req, timeout=20) as r:
    body = json.loads(r.read().decode())
print("quoteMode", body.get("quoteMode"), "bookFeed", body.get("bookFeed"))
for p in body.get("pairs") or []:
    if p.get("symbol") in ("TONUSDT", "NVDAUSDT", "XAGUSDT", "BTCUSDT"):
        print(p.get("market"), p.get("symbol"), p.get("status"), p.get("bookSource"), p.get("reason"), p.get("lastError"), "orders", p.get("openOrders"))
print("DBG_DONE")
