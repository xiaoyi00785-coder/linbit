#!/usr/bin/env python3
import os, shutil, subprocess
os.chdir("/opt/linbit")
subprocess.check_call(["tar", "xzf", "/tmp/linbit-live.tgz"])
print("EXTRACT_OK")
src_tk = "/opt/linbit/apps/web/features/TradeKit.tsx"
src_mk = "/opt/linbit/apps/web/lib/market.ts"
for root in ("/opt/linbit-fe", "/opt/linbit"):
    for src, rel in ((src_tk, "apps/web/features/TradeKit.tsx"), (src_mk, "apps/web/lib/market.ts")):
        dst = os.path.join(root, rel)
        if os.path.exists(os.path.dirname(dst)):
            if os.path.abspath(src) == os.path.abspath(dst):
                print("SAME", dst)
                continue
            shutil.copy(src, dst)
            print("COPIED", dst)
base = ["docker", "compose", "-p", "linbit", "-f", "docker-compose.yml", "-f", "docker-compose.override.yml", "-f", "deploy/mainnet/docker-compose.mainnet.yml", "--env-file", ".env"]
subprocess.check_call(base + ["build", "market-data", "futures"])
subprocess.check_call(base + ["up", "-d", "--no-deps", "market-data", "futures"])
print("BACKEND_UP")
web = ["docker", "compose", "-p", "linbit", "-f", "docker-compose.yml", "-f", "docker-compose.override.yml", "-f", "deploy/mainnet/docker-compose.mainnet.yml", "--profile", "chain"]
subprocess.check_call(web + ["build", "web"])
subprocess.check_call(web + ["up", "-d", "--no-deps", "web"])
print("LIVE_DEPLOY_DONE")
