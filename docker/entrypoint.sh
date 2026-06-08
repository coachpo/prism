#!/usr/bin/env bash
set -Eeuo pipefail

export PRISM_CONFIG_PATH="${PRISM_CONFIG_PATH:-/app/config/config.json}"
export PRISM_NGINX_PORT="${PRISM_NGINX_PORT:-8080}"
export PRISM_BACKEND_UPSTREAM_HOST="${PRISM_BACKEND_UPSTREAM_HOST:-127.0.0.1}"
export PRISM_BACKEND_UPSTREAM_PORT="${PRISM_BACKEND_UPSTREAM_PORT:-8000}"

config_dir="$(dirname "$PRISM_CONFIG_PATH")"
if ! mkdir -p "$config_dir"; then
  printf 'failed to create Prism config directory: %s\n' "$config_dir" >&2
  exit 1
fi

if ! touch "$config_dir/.write-check" 2>/dev/null; then
  printf 'Prism config directory is not writable by UID/GID 1000:1000: %s\n' "$config_dir" >&2
  exit 1
fi
rm -f "$config_dir/.write-check"

mkdir -p /tmp/nginx/client_body /tmp/nginx/proxy /tmp/nginx/fastcgi \
  /tmp/nginx/uwsgi /tmp/nginx/scgi

envsubst '${PRISM_NGINX_PORT} ${PRISM_BACKEND_UPSTREAM_HOST} ${PRISM_BACKEND_UPSTREAM_PORT}' \
  < /etc/prism/nginx.conf.template > /tmp/nginx.conf
nginx -t -c /tmp/nginx.conf

backend_pid=""
nginx_pid=""
terminating=0

shutdown() {
  terminating=1
  trap - TERM INT QUIT
  if [ -n "$backend_pid" ] && kill -0 "$backend_pid" 2>/dev/null; then
    kill -TERM "$backend_pid" 2>/dev/null || true
  fi
  if [ -n "$nginx_pid" ] && kill -0 "$nginx_pid" 2>/dev/null; then
    kill -TERM "$nginx_pid" 2>/dev/null || true
  fi
}

trap shutdown TERM INT QUIT

prism-backend &
backend_pid=$!
nginx -c /tmp/nginx.conf -g 'daemon off;' &
nginx_pid=$!

exited_pid=""
set +e
wait -n -p exited_pid "$backend_pid" "$nginx_pid"
status=$?
set -e

if [ "$terminating" -eq 1 ]; then
  wait "$backend_pid" "$nginx_pid" 2>/dev/null || true
  exit 0
fi

if [ "$exited_pid" = "$backend_pid" ]; then
  printf 'prism-backend exited unexpectedly with status %s\n' "$status" >&2
else
  printf 'nginx exited unexpectedly with status %s\n' "$status" >&2
fi

shutdown
wait "$backend_pid" "$nginx_pid" 2>/dev/null || true

if [ "$status" -eq 0 ]; then
  exit 1
fi
exit "$status"
