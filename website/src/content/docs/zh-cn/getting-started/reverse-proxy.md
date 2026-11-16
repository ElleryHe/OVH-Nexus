---
title: 反向代理与 HTTPS 配置
description: 使用 Caddy、Nginx 或 Cloudflare Tunnel 为 OVH Nexus 配置安全的 HTTPS 访问。
---

当通过公网访问 OVH Nexus 时，**强烈建议配置 HTTPS 反向代理**，开启 `.env` 中的 `COOKIE_SECURE=true`，并配置 `TRUSTED_PROXIES` 以便准确识别真实客户端 IP 与执行防暴力破解限流。

## 1. 在 `.env` 中启用安全 Cookie 与受信代理列表

修改 `.env` 并重启容器：

```ini
COOKIE_SECURE=true
# 仅信任本机反向代理及/或 Docker 网桥段传递的 X-Forwarded-For / X-Real-IP 请求头
TRUSTED_PROXIES=127.0.0.1,172.16.0.0/12
```

```bash
docker compose up -d
```

同时建议在 `docker-compose.yml` 中仅将容器 `8080` 端口绑定在宿主机回环地址：

```yaml
    ports:
      - "127.0.0.1:20000:8080"
```

:::caution[`TRUSTED_PROXIES` 与真实 IP 识别机制]
OVH Nexus **仅在**直接 TCP 来源 IP 命中 `TRUSTED_PROXIES` 白名单时才会采信 `X-Real-IP` 与 `X-Forwarded-For` 请求头。若 `TRUSTED_PROXIES` 留空，系统将忽略代理转发头以防伪造 IP；此时所有经反代访问的用户会共享同一个代理 IP 的登录限流额度（每 IP 300 秒内最多允许 5 次失败尝试）。
:::

## 2. 常见反向代理配置示例

### 方案 A：Caddy（自动申请与续期证书）

在 `/etc/caddy/Caddyfile` 中添加：

```txt
nexus.example.com {
    reverse_proxy 127.0.0.1:20000 {
        header_up X-Real-IP {remote_host}
    }
}
```

重载 Caddy：

```bash
sudo systemctl reload caddy
```

### 方案 B：Nginx + Let's Encrypt

请注意 `/api/logs/stream` 使用 **Server-Sent Events (SSE)**（`text/event-stream`），为该路径关闭 `proxy_buffering` 可确保实时日志零延迟推流：

```nginx
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name nexus.example.com;

    ssl_certificate     /etc/letsencrypt/live/nexus.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/nexus.example.com/privkey.pem;

    location /api/logs/stream {
        proxy_pass http://127.0.0.1:20000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 3600s;
    }

    location / {
        proxy_pass http://127.0.0.1:20000;
        proxy_http_version 1.1;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 支持 WebSocket / xterm.js 串口控制台
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
}
```

### 方案 C：Cloudflare Tunnel (`cloudflared`)

如果您的部署环境没有公网 IP（如家庭 NAS 或内网主机），或者希望宿主机零开放入站端口：

1. 在 Cloudflare Zero Trust 控制台中将 Public Hostname（例如 `nexus.example.com`）指向 `http://127.0.0.1:20000`。
2. 在 `.env` 中设置 `COOKIE_SECURE=true`，并将 `cloudflared` 所在源 IP/CIDR（如 `127.0.0.1` 或 Docker 网段）加入 `TRUSTED_PROXIES`。
3. 还可以进一步为该域名启用 **Cloudflare Access**（邮箱 OTP 或 GitHub SSO），在流量到达容器前增加一层零信任认证防护。
