# Batch11 device unblock notes

## Working recovery for ADB boot (no wipe)
Command (official `-accel off`; Cursor forum recommended on Bot computer):
```
DISPLAY=:1 emulator -avd easicoin_ref_api34 -port 5554 -no-audio \
  -gpu swiftshader_indirect -memory 2048 -cores 2 -no-boot-anim \
  -no-snapshot-load -no-snapshot-save -accel off
```
- First software boot: `Boot completed in 190912 ms`; ADB `device`; packages `click.cowa.linbit` + `io.easiex.app` present; userdata preserved.
- Log: `batch11-emulator-accel-off.log` / `batch11-emulator-accel-off-xvfb.log`

## Not usable for page screenshots this turn
- Framebuffer/X11 window renders green/corrupt under TCG+swiftshader (adb screencap ~15–31KB solid-ish; X11 grab same avg RGB).
- After ~9 min uptime: **System UI isn't responding** ANR; uiautomator only sees ANR dialog.
- Earlier hierarchy dump (before ANR) showed live spot trade UI texts — see `ours-home.xml` / prior pull.

## KVM path blocked (host)
- `dmesg`: `kernel BUG at arch/x86/kvm/x86.c:689!` in `kvm_arch_vcpu_create` (batch11-kvm-dmesg.txt).
- ACL `user:box:rw` gone; `sg pulse-access` makes open/accel-check OK but vCPU create still BUG.

## Sources
- Emulator CLI: `-accel off` valid (“Disables acceleration entirely”) via `emulator -help-accel`.
- https://developer.android.com/studio/run/emulator-acceleration
- https://forum.cursor.com/t/bots-computer-box-kvm-crashes-host-kernel-update-bots-computer-doesnt-actually-reboot/175743 (staff: known KVM crash; use `-accel off`; Update Bot's Computer rebuilds box)
- debugging-the-box.md: recover via Update Bot's Computer (not Reset); packages need reinstall after update.

## ChatAccessTest CI failure (fixed in same PR head)
- Symptom: `ConcurrentModificationException` in `waitUntil { api.requests.any { … } }` (CI run on `cdb9f6d`).
- Cause: test fixture `MockEvidenceApi.requests` used `Collections.synchronizedList`; iterators are not locked. MockWebServer dispatcher appends concurrently → CME. **Not a production app concurrency bug.**
- Fix: `CopyOnWriteArrayList` for request/mutation logs; ChatAccessTest uses `toList()` snapshots; `MockEvidenceApiConcurrencyTest` (+ 5× repeat).

## APK for user device check
- Deliverable is the project **debug** APK (release keystore not configured). Same Android Debug cert as prior batch2 debug APK on this box.
