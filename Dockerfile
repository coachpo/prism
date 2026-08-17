FROM golang:1.26.6-bookworm AS backend-builder

WORKDIR /app/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/cmd ./cmd
COPY backend/internal ./internal
RUN CGO_ENABLED=0 go build -o /out/prism-backend ./cmd/prism-backend

FROM node:26-alpine AS frontend-builder

WORKDIR /src/frontend

RUN apk add --no-cache libc6-compat \
    && corepack enable \
    && corepack prepare pnpm@10.30.1 --activate

ARG VITE_API_BASE=
ARG VITE_GIT_RUN_NUMBER=local
ARG VITE_GIT_REVISION=unknown

COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY frontend/ ./
RUN if [ -n "$VITE_API_BASE" ]; then export VITE_API_BASE; fi; \
    export VITE_GIT_RUN_NUMBER="$VITE_GIT_RUN_NUMBER"; \
    export VITE_GIT_REVISION="$VITE_GIT_REVISION"; \
    pnpm run build; \
    mkdir -p /out/html; \
    cp -R dist/. /out/html/

FROM debian:bookworm-slim AS runtime

WORKDIR /app/backend

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl gettext-base nginx bash \
    && groupadd --gid 1000 prism \
    && useradd --uid 1000 --gid 1000 --home-dir /app/backend --shell /bin/sh --no-create-home prism \
    && mkdir -p /app/config /app/backend /etc/prism /usr/share/nginx/html /tmp/nginx \
        /var/cache/nginx /var/log/nginx /var/lib/nginx \
    && chown -R prism:prism /app/config /app/backend /usr/share/nginx/html /tmp/nginx \
        /var/cache/nginx /var/log/nginx /var/lib/nginx \
    && rm -rf /var/lib/apt/lists/*

COPY --from=backend-builder /out/prism-backend /usr/local/bin/prism-backend
COPY backend/VERSION ./VERSION
COPY backend/migrations ./migrations
COPY --from=frontend-builder /out/html/ /usr/share/nginx/html/
COPY docker/nginx.conf.template /etc/prism/nginx.conf.template
COPY docker/entrypoint.sh /usr/local/bin/prism-entrypoint

RUN chmod 0755 /usr/local/bin/prism-entrypoint \
    && chown -R prism:prism /app/config /app/backend /usr/share/nginx/html

ENV PRISM_CONFIG_PATH=/app/config/config.json \
    PRISM_NGINX_PORT=8080 \
    PRISM_BACKEND_UPSTREAM_HOST=127.0.0.1 \
    PRISM_BACKEND_UPSTREAM_PORT=8000

EXPOSE 8080

USER prism:prism
ENTRYPOINT ["prism-entrypoint"]
