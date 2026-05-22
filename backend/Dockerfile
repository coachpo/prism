FROM golang:1.26.2-bookworm AS builder

WORKDIR /app/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/cmd ./cmd
COPY backend/internal ./internal
RUN CGO_ENABLED=0 go build -o /out/prism-backend ./cmd/prism-backend

FROM debian:bookworm-slim

WORKDIR /app/backend

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && groupadd --gid 1000 prism \
    && useradd --uid 1000 --gid 1000 --home-dir /app/backend --shell /bin/sh --no-create-home prism \
    && mkdir -p /app/config \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/prism-backend /usr/local/bin/prism-backend
COPY backend/VERSION ./VERSION
COPY backend/migrations ./migrations

RUN chown -R prism:prism /app/config /app/backend

ENV PRISM_CONFIG_PATH=/app/config/config.json

EXPOSE 8000

USER prism:prism
CMD ["prism-backend"]
