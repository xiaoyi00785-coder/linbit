# nginx ↔ gateway (testnet cowa.click)

Gateway access log: `GATEWAY_ACCESS_LOG` (default `true`; `false|0|off` disables).
One JSON line per request on stdout (`docker compose logs gateway`):
`method host path query(redacted) status latency_ms bytes ip request_id ua uid upgrade`.
`/healthz` is not logged. Client IP = `CF-Connecting-IP` > first `X-Forwarded-For` > `X-Real-IP` > peer.
An inbound `X-Request-Id` (e.g. nginx `$request_id`) is reused and echoed back.

```nginx
# http {} level
map $http_upgrade $connection_upgrade { default upgrade; '' close; }
log_format linbit '$remote_addr host=$host "$request" $status $body_bytes_sent '
                  'rt=$request_time rid=$request_id ua="$http_user_agent"';

# server { server_name api.cowa.click; ... }
access_log /var/log/nginx/linbit-access.log linbit;
location = /ws {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Request-Id $request_id;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Request-Id $request_id;
}
```

The web build bakes `NEXT_PUBLIC_WS_URL=wss://api.cowa.click/ws`; without it the
client falls back to `wss://<page host>/gw/ws` (Next rewrites cannot proxy WS),
so always set the build arg.
