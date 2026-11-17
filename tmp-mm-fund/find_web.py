#!/usr/bin/env python3
import json, subprocess
raw = subprocess.check_output(["docker", "inspect", "linbit-web-1"], text=True)
info = json.loads(raw)[0]
labels = info["Config"]["Labels"] or {}
print("IMAGE", info["Config"]["Image"])
for k in sorted(labels):
    if "compose" in k or "workdir" in k or "dockerfile" in k:
        print(k, labels[k])
print("WEB_FIND_DONE")
