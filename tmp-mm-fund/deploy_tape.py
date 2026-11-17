#!/usr/bin/env python3
import os, subprocess, time
os.chdir("/opt/linbit")
subprocess.check_call(["tar", "xzf", "/tmp/linbit-tape.tgz"])
print("EXTRACT_OK")
cmd = ["docker", "compose", "-f", "docker-compose.yml", "-f", "docker-compose.override.yml", "-f", "deploy/mainnet/docker-compose.mainnet.yml", "--env-file", ".env"]
subprocess.check_call(cmd + ["build", "market-data", "futures"])
subprocess.check_call(cmd + ["up", "-d", "--no-deps", "--force-recreate", "market-data", "futures"])
time.sleep(6)
print(subprocess.check_output(["docker", "ps", "--filter", "name=linbit-market-data-1", "--filter", "name=linbit-futures-1", "--format", "{{.Names}} {{.Status}}"], text=True))
print("TAPE_DEPLOY_DONE")
