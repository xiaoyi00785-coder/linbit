#!/usr/bin/env python3
import json, subprocess, urllib.request

def ip(name):
    return subprocess.check_output(["docker","inspect","-f","{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", name], text=True).split()[0]

md = "http://%s:8085" % ip("linbit-market-data-1")
fu = "http://%s:8089" % ip("linbit-futures-1")

def get(url):
    with urllib.request.urlopen(url, timeout=20) as r:
        return json.loads(r.read().decode())

k = get(md + "/market/klines?symbol=BTCUSDT&interval=15m&limit=3")
print("SPOT_K", len(k), "src", k[-1].get("source"), "vol", k[-1].get("volume"), "close", k[-1].get("close"))
t = get(md + "/market/trades?symbol=BTCUSDT&limit=2")
print("SPOT_T", t[0])
fk = get(fu + "/futures/market/klines?symbol=BTCUSDT&interval=15m&limit=3")
print("FUT_K", fk.get("source"), "n", len(fk.get("klines") or []), "last", (fk.get("klines") or [{}])[-1])
ft = get(fu + "/futures/market/trades?symbol=BTCUSDT&limit=2")
print("FUT_T", (ft.get("trades") or [None])[0])
ak = get(md + "/market/klines?symbol=AAPLUSDT&interval=15m&limit=1")
print("AAPL", ak[-1].get("source"), ak[-1].get("volume"), ak[-1].get("close"))
print("--- web ---")
print(subprocess.check_output("docker ps --format '{{.Names}} {{.Image}}' | head -40", shell=True, text=True))
print(subprocess.check_output("ls /var/www 2>/dev/null; ls /opt 2>/dev/null | head", shell=True, text=True))
print("VERIFY_TAPE_DONE")
