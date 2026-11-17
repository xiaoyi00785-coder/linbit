#!/usr/bin/env python3
import os, subprocess
os.chdir("/opt/linbit")
subprocess.check_call(["tar", "xzf", "/tmp/linbit-webui.tgz"])
base = ["docker", "compose", "-p", "linbit", "-f", "docker-compose.yml", "-f", "docker-compose.override.yml", "-f", "deploy/mainnet/docker-compose.mainnet.yml", "--env-file", ".env"]
subprocess.check_call(base + ["build", "web"])
subprocess.check_call(base + ["up", "-d", "--no-deps", "web"])
print("WEBUI_DONE")
