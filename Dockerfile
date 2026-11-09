FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder

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

WORKDIR /app

RUN apt-get update && \
    apt-get install -y --no-install-recommends nginx ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY backend/requirements.txt /app/backend/requirements.txt
RUN pip install --no-cache-dir -r /app/backend/requirements.txt

COPY backend/*.py /app/backend/
COPY backend/routes /app/backend/routes
COPY backend/notifications /app/backend/notifications

RUN mkdir -p /app/backend/data /app/backend/cache /app/backend/logs

COPY --from=frontend-builder /app/dist /usr/share/nginx/html
COPY nginx/nginx.conf /etc/nginx/nginx.conf

RUN echo '#!/bin/sh\n\
PORT="19998"\n\
cd /app/backend && python app.py &\n\
nginx -g "daemon off;"\n' > /start.sh && chmod +x /start.sh

EXPOSE 80

ENV PYTHONUNBUFFERED=1
ENV PORT=19998

CMD ["/start.sh"]
