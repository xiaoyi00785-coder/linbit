#!/usr/bin/env python3
import subprocess
print(subprocess.check_output(["docker", "logs", "--since", "3m", "linbit-market-data-1"], text=True, stderr=subprocess.STDOUT)[-2500:])
print("STREAM_LOG_DONE")
