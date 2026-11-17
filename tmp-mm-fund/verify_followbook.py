#!/usr/bin/env python3
import json, subprocess, urllib.request, time

tok = None
for line in open("/opt/linbit/.env"):
    if line.startswith("INTERNAL_TOKEN="):
        tok = line.split("=", 1)[1].strip().strip("'\"")
ip = subprocess.check_output(
    ["docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", "linbit-marketmaker-1"],
    text=True,
).split()[0]
time.sleep(8)
req = urllib.request.Request(
    "http://%s:8093/internal/mm/status" % ip,
    headers={"X-Internal-Token": tok},
)
with urllib.request.urlopen(req, timeout=20) as r:
    body = json.loads(r.read().decode())
print("quoteMode", body.get("quoteMode"))
print("bookFeed", body.get("bookFeed"))
print("priceFeed", body.get("priceFeed"))
print("running", body.get("running"), "enabled", body.get("enabled"))
print("feedDown", body.get("feedDownPairs"))
for p in body.get("pairs") or []:
    print("%s/%s status=%s book=%s src=%s orders=%s reason=%s err=%s" % (
        p.get("market"), p.get("symbol"), p.get("status"), p.get("bookSource"),
        p.get("source"), p.get("openOrders"), p.get("reason"), (p.get("lastError") or "")[:80]))
print("VERIFY_DONE")
