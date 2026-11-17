#!/usr/bin/env python3
import os, re, subprocess, time

ROOT = "/opt/linbit"
os.chdir(ROOT)
subprocess.check_call([
    "tar", "xzf", "/tmp/linbit-mm-followbook.tgz",
    "services/go/marketmaker/book.go",
    "services/go/marketmaker/bookfeed.go",
    "services/go/marketmaker/book_test.go",
    "services/go/marketmaker/config.go",
    "services/go/marketmaker/maker.go",
    "services/go/marketmaker/server.go",
    "services/go/marketmaker/main.go",
    "services/go/marketmaker/quote.go",
    "services/go/marketmaker/quote_test.go",
])
print("EXTRACT_OK")

def ensure_env(path, key, val):
    if not os.path.exists(path):
        print("SKIP", path)
        return
    t = open(path).read()
    if re.search(rf"^{re.escape(key)}=", t, re.M):
        t = re.sub(rf"^{re.escape(key)}=.*$", f"{key}={val}", t, flags=re.M)
    else:
        t = t.rstrip() + f"\n{key}={val}\n"
    open(path, "w").write(t)
    print("ENV", path, key, val)

for p in (".env",):
    ensure_env(p, "MM_BOOK_FEED", "binance,okx,bybit")
    ensure_env(p, "MM_BOOK_SCALE", "1")
    ensure_env(p, "MM_BOOK_TTL_MS", "1000")

cmd = ["docker", "compose", "-f", "docker-compose.yml"]
if os.path.exists("docker-compose.override.yml"):
    cmd += ["-f", "docker-compose.override.yml"]
if os.path.exists("deploy/mainnet/docker-compose.mainnet.yml"):
    cmd += ["-f", "deploy/mainnet/docker-compose.mainnet.yml"]
cmd += ["--env-file", ".env"]
print("BUILD", " ".join(cmd))
subprocess.check_call(cmd + ["build", "marketmaker"])
subprocess.check_call(cmd + ["up", "-d", "--no-deps", "--force-recreate", "marketmaker"])
time.sleep(5)
print(subprocess.check_output(["docker", "ps", "--filter", "name=linbit-marketmaker-1", "--format", "{{.Status}}"], text=True))
print("DEPLOY_DONE")
