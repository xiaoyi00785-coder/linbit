#!/usr/bin/env python3
import json, urllib.request, subprocess

def get(url):
    try:
        with urllib.request.urlopen(url, timeout=15) as r:
            raw = r.read().decode()
            return r.status, raw[:700]
    except Exception as e:
        return 0, str(e)

# gateway is published? try host nginx and docker IPs
ips = {}
for name, port in (("linbit-gateway-1", None), ("linbit-market-data-1", 8085), ("linbit-futures-1", 8089)):
    out = subprocess.check_output(
        ["docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", name],
        text=True,
    ).split()
    ips[name] = out[0] if out else ""
print("IPS", ips)
md = "http://%s:8085" % ips["linbit-market-data-1"]
fu = "http://%s:8089" % ips["linbit-futures-1"]
for u in (
    md + "/market/klines?symbol=BTCUSDT&interval=15m&limit=2",
    md + "/market/trades?symbol=BTCUSDT&limit=3",
    md + "/market/ticker?symbol=BTCUSDT",
    fu + "/futures/market/klines?symbol=BTCUSDT&interval=15m&limit=2",
    fu + "/futures/market/trades?symbol=BTCUSDT&limit=3",
    fu + "/futures/market/ticker?symbol=BTCUSDT",
):
    st, body = get(u)
    print("URL", u)
    print(st, body.replace("\n", " ")[:500])
    print("---")

sqls = [
    "SELECT count(*) FROM trades WHERE created_at > now() - interval '30 minutes';",
    "SELECT symbol, count(*) FROM trades WHERE created_at > now() - interval '30 minutes' GROUP BY 1 ORDER BY 2 DESC LIMIT 8;",
    "SELECT count(*) FROM fut_trades WHERE created_at > now() - interval '30 minutes';",
    "SELECT symbol, count(*) FROM fut_trades WHERE created_at > now() - interval '30 minutes' GROUP BY 1 ORDER BY 2 DESC LIMIT 8;",
    "SELECT max(created_at) FROM trades;",
    "SELECT max(created_at) FROM fut_trades;",
]
for sql in sqls:
    print("SQL", sql)
    print(subprocess.check_output(["docker", "exec", "linbit-postgres-1", "psql", "-U", "linbit", "-d", "linbit", "-c", sql], text=True))

print("--- mm logs taker ---")
logs = subprocess.check_output(["docker", "logs", "--since", "20m", "linbit-marketmaker-1"], text=True, stderr=subprocess.STDOUT)
lines = [ln for ln in logs.splitlines() if "taker" in ln.lower() or "take " in ln.lower() or "error" in ln.lower()]
print("\n".join(lines[-40:]) or "(no taker lines)")
print("TAPE_DONE")
