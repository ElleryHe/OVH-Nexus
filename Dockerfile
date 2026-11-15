FROM --platform=$BUILDPLATFORM node:26-alpine AS frontend-builder

WORKDIR /app

COPY package*.json ./
RUN npm ci

COPY src ./src
COPY public ./public
COPY index.html ./
COPY vite.config.ts ./
COPY tsconfig*.json ./
COPY tailwind.config.ts ./
COPY postcss.config.js ./
COPY components.json ./
COPY eslint.config.js ./

RUN npm run build

FROM python:3.11-slim

LABEL org.opencontainers.image.title="OVH Nexus" \
      org.opencontainers.image.description="Self-hosted OVHcloud control, automated bare-metal/VPS procurement & privacy-shielded MCP AI gateway console" \
      org.opencontainers.image.url="https://ovh-nexus.ellery.me" \
      org.opencontainers.image.documentation="https://ovh-nexus.ellery.me" \
      org.opencontainers.image.source="https://github.com/ElleryHe/OVH-Nexus" \
      org.opencontainers.image.licenses="AGPL-3.0-only"

WORKDIR /app

RUN apt-get update && \
    apt-get install -y --no-install-recommends nginx ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY backend/requirements.txt /app/backend/requirements.txt
RUN pip install --no-cache-dir -r /app/backend/requirements.txt

COPY backend/*.py /app/backend/
COPY backend/routes /app/backend/routes
COPY backend/notifications /app/backend/notifications
COPY backend/mcp /app/backend/mcp

RUN mkdir -p /app/backend/data /app/backend/cache /app/backend/logs

COPY --from=frontend-builder /app/dist /usr/share/nginx/html
COPY nginx/nginx.conf /etc/nginx/nginx.conf

RUN printf '#!/bin/sh\n\
set -e\n\
export PORT="${PORT:-19998}"\n\
cd /app/backend && python app.py &\n\
APP_PID=$!\n\
nginx -g "daemon off;" &\n\
NGINX_PID=$!\n\
trap "kill -TERM $APP_PID $NGINX_PID 2>/dev/null || true; exit 0" INT TERM\n\
while kill -0 "$APP_PID" 2>/dev/null && kill -0 "$NGINX_PID" 2>/dev/null; do\n\
  sleep 2\n\
done\n\
kill -TERM "$APP_PID" "$NGINX_PID" 2>/dev/null || true\n\
exit 1\n' > /start.sh && chmod +x /start.sh

EXPOSE 80

ENV PYTHONUNBUFFERED=1
ENV PORT=19998

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:80/api/health', timeout=4)" || exit 1

CMD ["/start.sh"]
