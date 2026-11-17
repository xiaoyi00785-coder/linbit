#!/usr/bin/env python3
import urllib.request

def get(url):
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "linbit-check"})
        with urllib.request.urlopen(req, timeout=15) as r:
            return r.status, r.geturl(), r.read()[:400].decode(errors="replace")
    except Exception as e:
        return 0, url, str(e)

for u in (
    "https://api.cowa.click/market/klines?symbol=BTCUSDT&interval=15m&limit=1",
    "https://api.cowa.click/market/trades?symbol=BTCUSDT&limit=1",
    "https://api.cowa.click/futures/market/klines?symbol=BTCUSDT&interval=15m&limit=1",
    "https://api.cowa.click/futures/market/trades?symbol=BTCUSDT&limit=1",
    "https://www.cowa.click/market/klines?symbol=BTCUSDT&interval=15m&limit=1",
    "https://www.cowa.click/",
):
    st, final, body = get(u)
    print(st, final)
    print(body.replace("\n", " ")[:280])
    print("---")
print("PUB_DONE")
