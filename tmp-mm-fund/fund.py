#!/usr/bin/env python3
"""Credit mm:main 100M USDT plus 100M USDT-eq of every MM base; fund futures + taker."""
import json, os, re, subprocess, time, urllib.error, urllib.request

ROOT = "/opt/linbit"
USDT = "100000000"
BASE_USDT = "100000000"
FUT_IN = "50000000"
TAKER_USDT = "20000000"
MAX = "1000000000"
FALLBACK_ASSETS = [
    "BTC", "ETH", "SOL", "BNB", "TRX", "XRP", "DOGE", "ARB", "AVAX", "LINK", "TON",
    "AAPL", "TSLA", "NVDA", "MSFT", "GOOGL", "AMZN", "META", "COIN", "SPY", "PAXG", "XAUT",
    "WTI", "XAG",
]


def sh(*args, check=True):
    r = subprocess.run(args, check=False, text=True, capture_output=True)
    if check and r.returncode != 0:
        print("CMD_FAIL", args, r.returncode, r.stdout, r.stderr)
        raise SystemExit(r.returncode)
    return r


def set_env(path, key, val):
    p = os.path.join(ROOT, path) if not path.startswith("/") else path
    if not os.path.exists(p):
        print("SKIP_ENV", p)
        return
    t = open(p).read()
    if re.search(rf"^{re.escape(key)}=", t, re.M):
        t = re.sub(rf"^{re.escape(key)}=.*$", f"{key}={val}", t, flags=re.M)
    else:
        t = t.rstrip() + f"\n{key}={val}\n"
    open(p, "w").write(t)
    print("SET", p, key, val)


def bump_overlay():
    for rel in (
        "deploy/mainnet/docker-compose.mainnet.yml",
        "docker-compose.override.yml",
        "docker-compose.yml",
    ):
        p = os.path.join(ROOT, rel)
        if not os.path.exists(p):
            continue
        t = open(p).read()
        orig = t
        for key in ("MM_SEED_MAX", "ADMIN_MM_SEED_MAX"):
            t = re.sub(
                rf"(^\s*{key}:\s*)(\"[0-9]+\"|\$\{{{key}:-[0-9]+\}}|[0-9]+)\s*$",
                rf'\1"{MAX}"',
                t,
                flags=re.M,
            )
        if t != orig:
            open(p, "w").write(t)
            print("PATCH_YML", p)
        else:
            print("YML_UNCHANGED", p)


def token():
    for p in (ROOT + "/.env", ROOT + "/deploy/mainnet/.env.mainnet"):
        if not os.path.exists(p):
            continue
        for line in open(p):
            if line.startswith("INTERNAL_TOKEN="):
                return line.split("=", 1)[1].strip().strip("'\"")
    raise SystemExit("no INTERNAL_TOKEN")


def ip(container):
    out = sh("docker", "inspect", "-f",
             "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", container).stdout.strip()
    for x in out.split():
        if x:
            return x
    raise SystemExit("no ip for " + container)


def http(method, url, headers, body=None, timeout=30):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method=method)
    for k, v in headers.items():
        req.add_header(k, v)
    if body is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            raw = r.read().decode()
            try:
                return r.status, json.loads(raw) if raw else {}
            except json.JSONDecodeError:
                return r.status, {"raw": raw}
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw) if raw else {"error": str(e)}
        except json.JSONDecodeError:
            return e.code, {"error": raw}


def compose():
    cmd = ["docker", "compose", "-f", "docker-compose.yml"]
    if os.path.exists(ROOT + "/docker-compose.override.yml"):
        cmd += ["-f", "docker-compose.override.yml"]
    if os.path.exists(ROOT + "/deploy/mainnet/docker-compose.mainnet.yml"):
        cmd += ["-f", "deploy/mainnet/docker-compose.mainnet.yml"]
    if os.path.exists(ROOT + "/deploy/mainnet/.env.mainnet"):
        cmd += ["--env-file", "deploy/mainnet/.env.mainnet"]
    elif os.path.exists(ROOT + "/.env"):
        cmd += ["--env-file", ".env"]
    return cmd


def bases():
    r = sh("docker", "exec", "linbit-postgres-1", "psql", "-U", "linbit", "-d", "linbit", "-tA", "-c",
           "SELECT DISTINCT regexp_replace(symbol, 'USDT$', '') FROM mm_pairs WHERE symbol LIKE '%USDT' ORDER BY 1;")
    got = [x.strip() for x in r.stdout.splitlines() if x.strip() and x.strip() != "USDT"]
    print("PAIRS_BASES", got)
    return got or FALLBACK_ASSETS


def wait_ok(url, n=40):
    for i in range(n):
        try:
            st, body = http("GET", url, {})
            if st == 200:
                print("READY", url, body if isinstance(body, dict) else "")
                return
        except Exception as e:
            print("WAIT", url, i, e)
        time.sleep(1)
    raise SystemExit("not ready: " + url)


def main():
    os.chdir(ROOT)
    tok = token()
    bump_overlay()
    for p in (".env", "deploy/mainnet/.env.mainnet"):
        set_env(p, "MM_SEED_MAX", MAX)
        set_env(p, "ADMIN_MM_SEED_MAX", MAX)
    c = compose()
    print("COMPOSE", " ".join(c))
    r = sh(*(c + ["up", "-d", "--no-deps", "--force-recreate", "asset"]))
    print(r.stdout)
    print(r.stderr)
    time.sleep(3)
    hdr = {"X-Internal-Token": tok}
    asset = "http://%s:8082" % ip("linbit-asset-1")
    market = "http://%s:8085" % ip("linbit-market-data-1")
    fut = "http://%s:8089" % ip("linbit-futures-1")
    print("URLS", asset, market, fut)
    wait_ok(asset + "/healthz")

    def seed(asset_code, amount, ref):
        st, body = http("POST", asset + "/internal/mm/testnet-seed", hdr, {
            "userId": "mm:main", "asset": asset_code, "amount": str(amount), "ref": ref,
        })
        print("SEED", asset_code, amount, st, body)
        return st, body

    def index(sym):
        st, body = http("GET", market + "/market/index?symbol=" + sym, {})
        if st != 200:
            print("INDEX_FAIL", sym, st, body)
            return None
        px = (body or {}).get("indexPrice") or (body or {}).get("price")
        try:
            v = float(px)
        except (TypeError, ValueError):
            print("INDEX_BAD", sym, body)
            return None
        if v <= 0:
            return None
        return v

    seed("USDT", USDT, "mm_usdt_100m_v1")
    target = float(BASE_USDT)
    for a in bases():
        px = index(a + "USDT")
        if not px:
            continue
        qty = target / px
        if qty <= 0:
            continue
        amt = ("%.8f" % qty).rstrip("0").rstrip(".")
        if float(amt) > float(MAX):
            amt = MAX
            print("CAP_QTY", a, qty, "->", amt)
        seed(a, amt, "mm_base_100m_v1_" + a.lower())

    st, body = http("POST", fut + "/assets/transfer", {"X-User-Id": "mm:main"}, {
        "asset": "USDT", "amount": FUT_IN, "from": "spot", "to": "futures", "clientId": "mm-fut-100m-v1",
    })
    print("FUT_TRANSFER", st, body)

    st, body = http("POST", asset + "/internal/admin/adjust", hdr, {
        "userId": "taker:mm", "asset": "USDT", "account": "spot", "amount": TAKER_USDT, "ref": "taker-usdt-100m-v1",
    })
    print("TAKER_USDT", st, body)
    st, body = http("POST", fut + "/assets/transfer", {"X-User-Id": "taker:mm"}, {
        "asset": "USDT", "amount": "10000000", "from": "spot", "to": "futures", "clientId": "taker-fut-100m-v1",
    })
    print("TAKER_FUT", st, body)

    for sql in (
        "SELECT user_id, asset, free::text FROM balances WHERE user_id IN ('mm:main','taker:mm') AND free::numeric > 0 ORDER BY 1,2;",
        "SELECT user_id, balance::text, frozen::text FROM fut_accounts WHERE user_id IN ('mm:main','taker:mm');",
        "SELECT market, symbol FROM mm_pairs ORDER BY 1,2;",
    ):
        print(sql)
        print(sh("docker", "exec", "linbit-postgres-1", "psql", "-U", "linbit", "-d", "linbit", "-c", sql).stdout)
    print("DONE_SENTINEL")


if __name__ == "__main__":
    main()
