---
title: 反向代理与 HTTPS 配置
description: 使用 Nginx、Caddy 或 Cloudflare Tunnel 为 OVH Nexus 配置安全的 HTTPS 访问。
---

当通过公网访问 OVH Nexus 时，**强烈建议配置 HTTPS 反向代理**，并将 `.env` 中的 `COOKIE_SECURE` 设为 `true`，确保身份认证 Cookie（`ovh_session`）仅在加密的 TLS 连接上传输。

## 1. 在 `.env` 中启用安全 Cookie

修改 `.env` 并重启容器：

```ini
COOKIE_SECURE=true
```

```bash
docker compose up -d
```

同时建议在 `docker-compose.yml` 中仅将端口绑定在宿主机回环地址：

```yaml
    ports:
      - "127.0.0.1:20000:80"
```

:::caution[转发真实客户端 IP 的重要性]
OVH Nexus 依赖 `X-Real-IP` 和 `X-Forwarded-For` 请求头来识别客户端真实 IP，用于**登录防暴力破解限流（每 IP 300 秒内最多允许 5 次失败尝试）**，并阻止外部请求伪造本机直连专用的 `X-API-Key` 请求头。请确保在反向代理中正确传递这些请求头。
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

```nginx
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name nexus.example.com;

    ssl_certificate     /etc/letsencrypt/live/nexus.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/nexus.example.com/privkey.pem;

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
2. 在 `.env` 中设置 `COOKIE_SECURE=true`。
3. 还可以进一步为该域名启用 **Cloudflare Access**（邮箱 OTP 或 GitHub SSO），在流量到达容器前增加一层零信任认证防护。
