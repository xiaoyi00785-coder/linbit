#!/usr/bin/env python3
"""Pick bootable iPhone simulators for adapt profiles (CI helper).

Reads mobile/ci/adapt-device-matrix.json + simctl devices JSON.
Writes GITHUB_ENV ADAPT_PROFILE_JSON=... list of {id,udid,name,os,content_size,tag}.
Never invents a UDID for ci_supported=false profiles.
"""
from __future__ import annotations
import json, os, re, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
matrix = json.loads((ROOT / "mobile/ci/adapt-device-matrix.json").read_text())
profile_name = os.environ.get("ADAPT_DEVICE_PROFILE", "se375_xxxl")
wanted_ids = matrix["workflow_profiles"].get(profile_name)
if not wanted_ids:
    sys.exit(f"::error::unknown adapt_device_profile={profile_name}")

sim_path = Path(os.environ.get("LINBIT_SIM_JSON", "/tmp/linbit-simulators.json"))
devices_by_runtime = json.loads(sim_path.read_text())["devices"]

def available():
    out = []
    for runtime, devices in devices_by_runtime.items():
        if "iOS" not in runtime:
            continue
        parts = runtime.split("iOS-")
        sim_os = parts[-1].replace("-", ".") if len(parts) > 1 else "latest"
        ver = tuple(int(x) for x in re.findall(r"\d+", sim_os)[:3]) or (0,)
        for d in devices:
            if d.get("isAvailable") is False:
                continue
            if d.get("availability") not in (None, "(available)"):
                continue
            name, udid = d.get("name", ""), d.get("udid", "")
            if not udid or "iPhone" not in name:
                continue
            out.append((ver, name, sim_os, udid))
    return out

cands = available()
selected = []
for pid in wanted_ids:
    prof = matrix["profiles"][pid]
    if not prof.get("ci_supported"):
        print(f"skip unsupported profile {pid}: {prof.get('unsupported_reason','')}")
        continue
    prefer = list(prof.get("device_prefer") or []) + list(prof.get("fallback_prefer") or [])
    pick = None
    for rank, pref in enumerate(prefer):
        matched = [(ver, name, sim_os, udid) for ver, name, sim_os, udid in cands if pref in name]
        if matched:
            matched.sort(key=lambda x: tuple(-v for v in x[0]))
            pick = matched[0]
            break
    if not pick:
        # last resort any iPhone
        if not cands:
            sys.exit(f"::error::no iPhone simulator for profile {pid}")
        cands_sorted = sorted(cands, key=lambda x: tuple(-v for v in x[0]))
        pick = cands_sorted[0]
        print(f"::warning::profile {pid} fell back to {pick[1]}")
    ver, name, sim_os, udid = pick
    tag = pid  # e.g. se375_xxxl
    selected.append({
        "id": pid,
        "tag": tag,
        "udid": udid,
        "name": name,
        "os": sim_os,
        "content_size": prof["content_size"],
        "points_w": prof["points_w"],
        "scale": prof["scale"],
        "pixels_w": prof["pixels_w"],
        "pixels_h": prof["pixels_h"],
    })
    print(f"Selected {pid}: {name} iOS {sim_os} UDID {udid} "
          f"expect ~{prof['points_w']}pt@{prof['scale']}x ({prof['pixels_w']}x{prof['pixels_h']})")

if not selected:
    sys.exit("::error::no ci_supported adapt profiles selected")

# Primary sim for single build destination
primary = selected[0]
env_path = os.environ.get("GITHUB_ENV")
payload = json.dumps(selected, ensure_ascii=False)
out_json = Path(os.environ.get("ADAPT_PROFILES_OUT", "/tmp/adapt-profiles.json"))
out_json.write_text(payload + "\n", encoding="utf-8")
print(f"wrote {out_json}")
if env_path:
    with open(env_path, "a", encoding="utf-8") as f:
        f.write(f"SIM_NAME={primary['name']}\n")
        f.write(f"SIM_OS={primary['os']}\n")
        f.write(f"SIM_UDID={primary['udid']}\n")
        f.write(f"ADAPT_PROFILES_FILE={out_json}\n")
else:
    print(payload)
print(f"profiles_selected={len(selected)}")
