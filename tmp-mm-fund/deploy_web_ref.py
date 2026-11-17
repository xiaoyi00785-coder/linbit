#!/usr/bin/env python3
import os, shutil, subprocess
src = "/tmp/TradeKit.tsx"
if not os.path.exists(src):
    raise SystemExit("missing TradeKit")
for root in ("/opt/linbit-fe", "/opt/linbit"):
    dst = os.path.join(root, "apps/web/features/TradeKit.tsx")
    if not os.path.exists(os.path.dirname(dst)):
        print("SKIP", dst)
        continue
    if os.path.abspath(src) == os.path.abspath(dst):
        print("SAME", dst)
        continue
    shutil.copy(src, dst)
    print("COPIED", dst)
os.chdir("/opt/linbit")
cmd = ["docker", "compose", "-p", "linbit", "-f", "docker-compose.yml", "-f", "docker-compose.override.yml", "-f", "deploy/mainnet/docker-compose.mainnet.yml", "--profile", "chain"]
subprocess.check_call(cmd + ["build", "web"])
subprocess.check_call(cmd + ["up", "-d", "--no-deps", "web"])
print("WEB_REF_DONE")
