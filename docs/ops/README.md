# Linbit ops

This folder is the operator's reference for the mainnet production box
(74.113.233.169, Kamatera Tokyo).

- [`OPS_CHECKLIST.md`](OPS_CHECKLIST.md) — everything the operator has to
  configure by hand (Google OAuth, APNS, Resend templates, Telegram bot,
  monitoring), what env var it maps to, and how to smoke-test it.
- [`deploy.sh`](deploy.sh) — one-shot deploy with build retry, healthcheck,
  and automatic rollback. Install at `/root/deploy.sh` on the box.
- [`../../deploy/mainnet/.env.mainnet.example`](../../deploy/mainnet/.env.mainnet.example) —
  the production-flavored source of truth for every variable the mainnet
  stack reads, grouped by concern with defaults called out. The repo root
  keeps `.env.example` for a dev-friendly subset.

Day-to-day commands assume the SSH helper configured on the laptop:

```bash
~/.linbit-ops/n '<cmd>' < /dev/null       # run remote command
scp -o ControlPath=$HOME/.linbit-ops/cm-%r@%h <local> root@74.113.233.169:<remote>
```
