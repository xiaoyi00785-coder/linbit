# nginx 与 gateway（测试网 cowa.click）

Gateway 访问日志：`GATEWAY_ACCESS_LOG`（默认 `true`；`false|0|off` 关闭）。
每个请求在 stdout 打一行 JSON（`docker compose logs gateway`）：
`method host path query(redacted) status latency_ms bytes ip request_id ua uid upgrade`。
`/healthz` 不记。客户端 IP = `CF-Connecting-IP` > `X-Forwarded-For` 第一个 > `X-Real-IP` > 对端。
入站 `X-Request-Id`（例如 nginx `$request_id`）会被复用并回写。

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

Web 构建写入 `NEXT_PUBLIC_WS_URL=wss://api.cowa.click/ws`。不设则客户端退回 `wss://<page host>/gw/ws`（Next rewrite 不能代理 WS），构建参数必须带上。
