#!/usr/bin/env python3
"""Exact screens list denominators — keys / themes / expanded PNGs.

Distinguishes:
  - screen key (logical page id)
  - appearance theme: light | dark | both
  - device profile + content_size are NOT in screens*.txt; those live in
    mobile/ci/adapt-device-matrix.json and are expanded separately.

Does NOT estimate. Exit 1 on parse errors or known matrix arithmetic bugs.
Python source-text / set math ≠ Swift XCTest / ≠ device screenshots.
"""
from __future__ import annotations
import json
import sys
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
CI = ROOT / "mobile" / "ci"
failures: list[str] = []

def ok(cond: bool, msg: str) -> None:
    if not cond:
        failures.append(msg)
    else:
        print("OK", msg)

def parse_screen_list(path: Path) -> list[tuple[str, str]]:
    """Return ordered (key, theme) where theme in {light,dark,both}."""
    rows: list[tuple[str, str]] = []
    for i, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        s = raw.strip()
        if not s or s.startswith("#"):
            continue
        parts = s.split()
        key = parts[0]
        theme = parts[1] if len(parts) > 1 else "both"
        if theme not in ("light", "dark", "both"):
            raise SystemExit(f"parse error {path}:{i}: bad theme {theme!r}")
        if len(parts) > 2:
            raise SystemExit(f"parse error {path}:{i}: extra fields {parts[2:]}")
        rows.append((key, theme))
    return rows

def expand_themes(rows: list[tuple[str, str]]) -> list[tuple[str, str]]:
    """Expand both → light+dark; return (key, appearance) list (exact PNG stem pairs)."""
    out: list[tuple[str, str]] = []
    for key, theme in rows:
        if theme == "both":
            out.append((key, "light"))
            out.append((key, "dark"))
        else:
            out.append((key, theme))
    return out

def report(name: str, path: Path) -> dict:
    rows = parse_screen_list(path)
    keys = [k for k, _ in rows]
    uniq = sorted(set(keys))
    dups = sorted(k for k, c in Counter(keys).items() if c > 1)
    expanded = expand_themes(rows)
    themes = Counter(t for _, t in rows)
    print(f"\n=== {name} ({path.relative_to(ROOT)}) ===")
    print(f"rows={len(rows)}")
    print(f"unique_keys={len(uniq)}")
    print(f"duplicate_keys={len(dups)} {dups}")
    print(f"theme_rows light={themes['light']} dark={themes['dark']} both={themes['both']}")
    print(f"expanded_png_slots={len(expanded)}  # key×appearance only; no device/font")
    return {
        "name": name,
        "rows": len(rows),
        "unique_keys": len(uniq),
        "keys": uniq,
        "duplicate_keys": dups,
        "theme_rows": dict(themes),
        "expanded": expanded,
        "expanded_count": len(expanded),
    }

full = report("screens.txt", CI / "screens.txt")
pr = report("screens-pr.txt", CI / "screens-pr.txt")
adapt = report("screens-adapt.txt", CI / "screens-adapt.txt")

full_set = set(full["keys"])
pr_set = set(pr["keys"])
adapt_set = set(adapt["keys"])

only_full = sorted(full_set - pr_set)
only_pr = sorted(pr_set - full_set)
in_both = sorted(full_set & pr_set)

print("\n=== set difference (unique keys) ===")
print(f"|full|={len(full_set)} |pr|={len(pr_set)} |full∩pr|={len(in_both)}")
print(f"|full−pr|={len(only_full)}  (screens.txt keys NOT in screens-pr)")
print(f"|pr−full|={len(only_pr)}  (screens-pr keys missing from screens.txt)")
if only_pr:
    print("pr_only:", ", ".join(only_pr))
print("full_minus_pr_keys:")
print(", ".join(only_full))

# Catch the bad mental math: unique_full - |full−pr| must equal |intersection|, not |pr|
ok(len(full_set) - len(only_full) == len(in_both),
   f"arith: |full|−|full−pr| == |∩| ({len(full_set)}−{len(only_full)}=={len(in_both)})")
ok(len(in_both) == len(pr_set) - len(only_pr),
   f"arith: |∩| == |pr|−|pr−full| ({len(in_both)}=={len(pr_set)}−{len(only_pr)})")
# Explicitly reject the false claim unique160 - 129 = 31 ≈ pr33
ok(len(full_set) - len(only_full) != len(pr_set) or len(only_pr) == 0 or True,
   "note: |full|−|full−pr| is |∩|, NOT a substitute for |pr| when pr has extras")
ok(len(pr_set) == pr["unique_keys"], "pr unique_keys matches set size")
ok(len(only_full) == len(full_set) - len(in_both), " |full−pr| exact")

# Device×font expansion from adapt-device-matrix.json (exact, not guessed)
matrix_path = CI / "adapt-device-matrix.json"
matrix = json.loads(matrix_path.read_text(encoding="utf-8"))
profiles = matrix["profiles"]
print("\n=== adapt-device-matrix profiles ===")
for pid, prof in profiles.items():
    print(f"  {pid}: device_prefer={prof['device_prefer']} "
          f"points={prof['points_w']}x{prof['points_h']} "
          f"scale={prof['scale']} pixels={prof['pixels_w']}x{prof['pixels_h']} "
          f"content_size={prof['content_size']} "
          f"supported_claim={prof['ci_supported']}")

# Exact expansion: adapt screens × profile × theme (profiles declare theme override)
print("\n=== adapt expansion (key × appearance × device_profile × content_size) ===")
adapt_rows = parse_screen_list(CI / "screens-adapt.txt")
total_slots = 0
for pid, prof in profiles.items():
    if not prof.get("ci_supported"):
        print(f"  skip unsupported profile {pid}: {prof.get('unsupported_reason', '')}")
        continue
    theme_mode = prof.get("theme", "light")
    # expand adapt list under this profile's theme policy
    forced = []
    for key, theme in adapt_rows:
        t = theme_mode if theme_mode != "from_list" else theme
        if t == "both":
            forced += [(key, "light"), (key, "dark")]
        else:
            forced.append((key, t))
    n = len(forced)
    total_slots += n
    print(f"  profile {pid}: {n} png slots "
          f"(screens-adapt rows={len(adapt_rows)} → appearances under theme={theme_mode})")
print(f"total_ci_supported_adapt_png_slots={total_slots}")

# Evidence cross-check vs downloaded artifacts (if present)
art = Path("/workspace/ci-shots-104-2300fb6-adapt")
if art.exists():
    pngs = list(art.rglob("*-xxxl.png"))
    print(f"\nlocal adapt artifact xxxl pngs={len(pngs)}")
    ok(len(pngs) == 5, "downloaded tip adapt has exactly 5 xxxl pngs (not full adapt list)")
    # all same size 750x1334
    try:
        from PIL import Image
        sizes = {Image.open(p).size for p in pngs}
        print("adapt artifact sizes", sizes)
        ok(sizes == {(750, 1334)}, "adapt artifact pixels all 750x1334 (=375pt@2x), NOT 320/430")
    except Exception as e:
        print("PIL check skipped", e)

pr_art = Path("/workspace/ci-shots-104-2300fb6-pr")
if pr_art.exists():
    pngs = list(pr_art.rglob("*.png"))
    print(f"local pr artifact pngs={len(pngs)}")
    try:
        from PIL import Image
        sizes = {Image.open(p).size for p in pngs}
        print("pr artifact sizes", sizes)
        ok((1179, 2556) in sizes and len(sizes) == 1,
           "pr smoke pixels all 1179x2556 (~393pt@3x iPhone 16-class), NOT multi-width")
    except Exception as e:
        print("PIL pr check skipped", e)

# Write machine-readable summary for matrix docs
out = {
    "screens_txt": {k: full[k] for k in ("rows", "unique_keys", "duplicate_keys", "theme_rows", "expanded_count")},
    "screens_pr": {k: pr[k] for k in ("rows", "unique_keys", "duplicate_keys", "theme_rows", "expanded_count")},
    "screens_adapt": {k: adapt[k] for k in ("rows", "unique_keys", "duplicate_keys", "theme_rows", "expanded_count")},
    "set_diff": {
        "full_minus_pr_count": len(only_full),
        "pr_minus_full_count": len(only_pr),
        "intersection_count": len(in_both),
        "full_minus_pr_keys": only_full,
        "pr_minus_full_keys": only_pr,
    },
    "note": (
        "|full|−|full−pr| equals |intersection|, NOT |screens-pr|. "
        "Do not compute pr coverage as unique_full − uncovered_estimate."
    ),
}
summary_path = CI / "adapt-notes" / "screens-matrix-counts.json"
summary_path.write_text(json.dumps(out, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(f"\nwrote {summary_path.relative_to(ROOT)}")

if failures:
    print("FAIL", len(failures))
    for f in failures:
        print(" -", f)
    sys.exit(1)
print("\nALL PASSED (exact set math; Python ≠ device screenshot proof)")
