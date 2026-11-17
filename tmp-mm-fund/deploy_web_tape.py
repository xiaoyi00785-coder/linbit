#!/usr/bin/env python3
import os, shutil, subprocess
src = "/tmp/TradeKit.tsx"
if not os.path.exists(src):
    raise SystemExit("missing /tmp/TradeKit.tsx")
cands = [
    "/opt/linbit-fe/apps/web/features/TradeKit.tsx",
    "/opt/linbit/apps/web/features/TradeKit.tsx",
]
n = 0
for c in cands:
    if os.path.exists(c):
        shutil.copy(src, c)
        print("COPIED", c)
        n += 1
    else:
        print("MISSING", c)
if n == 0:
    raise SystemExit("no TradeKit on server")
os.chdir("/opt/linbit")
cmd = ["docker", "compose", "-p", "linbit", "-f", "docker-compose.yml", "-f", "docker-compose.override.yml", "-f", "deploy/mainnet/docker-compose.mainnet.yml", "--profile", "chain", "build", "web"]
print("BUILD", " ".join(cmd))
subprocess.check_call(cmd)
subprocess.check_call(["docker", "compose", "-p", "linbit", "-f", "docker-compose.yml", "-f", "docker-compose.override.yml", "-f", "deploy/mainnet/docker-compose.mainnet.yml", "--profile", "chain", "up", "-d", "--no-deps", "web"])
print("WEB_TAPE_DONE")
