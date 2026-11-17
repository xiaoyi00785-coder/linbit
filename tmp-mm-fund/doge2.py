#!/usr/bin/env python3
import json, subprocess, urllib.request

tok = None
for line in open("/opt/linbit/.env"):
    if line.startswith("INTERNAL_TOKEN="):
        tok = line.split("=", 1)[1].strip().strip("'\"")
ip = subprocess.check_output(
    ["docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", "linbit-asset-1"],
    text=True,
).split()[0]
body = json.dumps({
    "userId": "mm:main",
    "asset": "DOGE",
    "amount": "699813021",
    "ref": "mm_base_100m_v2_doge",
}).encode()
req = urllib.request.Request(
    "http://%s:8082/internal/mm/testnet-seed" % ip,
    data=body,
    method="POST",
    headers={"X-Internal-Token": tok, "Content-Type": "application/json"},
)
with urllib.request.urlopen(req, timeout=20) as r:
    print("DOGE2", r.status, r.read().decode())
print("DOGE2_DONE")
