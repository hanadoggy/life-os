# syntax=docker/dockerfile:1

FROM node:24-alpine AS frontend-builder

RUN corepack enable && corepack prepare pnpm@12.16.1 --activate || true

WORKDIR /app
COPY frontend/ ./

RUN if [ -f package.json ]; then \
      pnpm install --frozen-lockfile && pnpm build; \
    else \
      mkdir -p dist && cp index.html dist/index.html; \
    fi

FROM nginx:1.27-alpine

RUN apk add --no-cache openssl tzdata

COPY deploy/nginx/nginx.conf /etc/nginx/nginx.conf
COPY deploy/nginx/conf.d/lifeos.conf /etc/nginx/conf.d/default.conf
COPY deploy/nginx/10-init-dummy-cert.sh /docker-entrypoint.d/10-init-dummy-cert.sh
RUN chmod +x /docker-entrypoint.d/10-init-dummy-cert.sh

COPY --from=frontend-builder /app/dist /usr/share/nginx/html

EXPOSE 80 443

