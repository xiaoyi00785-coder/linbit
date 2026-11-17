#!/usr/bin/env python3
import json, urllib.request, subprocess, time

tok = None
for line in open("/opt/linbit/.env"):
    if line.startswith("INTERNAL_TOKEN="):
        tok = line.split("=", 1)[1].strip().strip("'\"")

def get(url):
    try:
        with urllib.request.urlopen(urllib.request.Request(url, headers={"User-Agent": "linbit-mm/1.0"}), timeout=15) as r:
            return r.status, r.read()[:400].decode()
    except Exception as e:
        return 0, str(e)

print("BN_FAPI_TON", get("https://fapi.binance.com/fapi/v1/depth?symbol=TONUSDT&limit=5"))
print("BN_FAPI_1000TON", get("https://fapi.binance.com/fapi/v1/depth?symbol=1000TONUSDT&limit=5"))
print("OKX_TON", get("https://www.okx.com/api/v5/market/books?instId=TON-USDT-SWAP&sz=5"))
print("BN_SPOT_TON", get("https://data-api.binance.vision/api/v3/depth?symbol=TONUSDT&limit=5"))

time.sleep(4)
ip = subprocess.check_output(
    ["docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", "linbit-marketmaker-1"],
    text=True,
).split()[0]
req = urllib.request.Request("http://%s:8093/internal/mm/status" % ip, headers={"X-Internal-Token": tok})
with urllib.request.urlopen(req, timeout=20) as r:
    body = json.loads(r.read().decode())
print("feedDown", body.get("feedDownPairs"), "n_pairs", len(body.get("pairs") or []))
paused = []
for p in body.get("pairs") or []:
    st = p.get("status")
    if st != "quoting":
        paused.append("%s/%s %s %s %s" % (p.get("market"), p.get("symbol"), st, p.get("reason"), (p.get("lastError") or "")[:60]))
    print("%s/%s %s book=%s orders=%s" % (p.get("market"), p.get("symbol"), st, p.get("bookSource"), p.get("openOrders")))
print("NOT_QUOTING", paused)
print("TON_DONE")
