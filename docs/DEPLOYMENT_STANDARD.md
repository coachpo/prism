# Deployment Standard: SPA + API Backend

A reusable guideline for deploying a single-page application (SPA) frontend with an API backend. Covers local development, Docker production deployment, and reverse proxy configuration.

---

## Architecture Overview

```
Local Development                    Production (Docker)
─────────────────                    ───────────────────

Browser                              Browser
  │                                    │
  ├──→ Frontend (localhost:3000)       ├──→ Reverse Proxy (:80/:443)
  │    Dev server (Vite, etc.)         │    ├─ /api/*  → backend:PORT
  │    No proxy — calls backend        │    └─ /*      → frontend:PORT
  │    directly via CORS               │
  └──→ Backend  (localhost:8000)       ├── backend  (expose only)
       App server (uvicorn, etc.)      └── frontend (expose only)
```

---

## 1. Local Development

### Rules

- Frontend and backend run as separate processes on separate ports.
- Frontend calls the backend directly — no dev-server proxy.
- Frontend uses an environment variable (e.g., `VITE_API_BASE`) set to the backend origin.
- Backend CORS allows `*` (or at minimum the frontend dev origin).
- Convention: frontend on port 3000, backend on port 8000.

### Example

```bash
# Terminal 1 — Backend
cd backend
uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload

# Terminal 2 — Frontend
cd frontend
VITE_API_BASE=http://localhost:8000 pnpm run dev --port 3000
```

The frontend at `http://localhost:3000` calls APIs at `http://localhost:8000`.
Other clients (curl, Postman, etc.) also call the backend at `http://localhost:8000`.

### What to Remove

- Dev-server proxy configuration (e.g., Vite `server.proxy`, CRA `proxy` field).
- Any variables that only exist to support the dev proxy (e.g., `VITE_PROXY_TARGET`).

---

## 2. Production Architecture

### Required Components

| Component | Role |
|---|---|
| Backend | API server (Docker container) |
| Frontend | Static build served by lightweight HTTP server (Docker container) |
| Reverse proxy | Path-based routing, TLS termination (Caddy recommended) |
| Persistent storage | Named Docker volume for database/data files |

### Rules

- Only the reverse proxy exposes host ports (80/443). Backend and frontend use `expose`, not `ports`.
- Reverse proxy routes by path prefix — API paths to backend, everything else to frontend.
- Same origin eliminates CORS for browser clients. Backend CORS can stay permissive for CLI/external consumers.
- Frontend image serves a static build via nginx:alpine or caddy:alpine. Server config is baked into the image.
- `depends_on` with `condition: service_healthy` ensures startup ordering.
- Named volumes for: database persistence, reverse proxy TLS certificates.

---

## 3. Required Files

| File | Purpose |
|---|---|
| `Caddyfile` | Reverse proxy routing rules |
| `docker-compose.yml` | Service orchestration |
| `.env.example` | Host-facing configuration template |
| `data/` | Persistent storage directory (optional, for bind mounts) |

---

## 4. Reverse Proxy Configuration (Caddyfile)

```caddyfile
{$DOMAIN:localhost} {
    encode gzip zstd

    # API paths → backend
    handle /api/* {
        reverse_proxy backend:{$BACKEND_PORT:8000}
    }

    # Additional backend paths (if any)
    # handle /v1/* {
    #     reverse_proxy backend:{$BACKEND_PORT:8000}
    # }

    # Everything else → frontend
    handle {
        reverse_proxy frontend:{$FRONTEND_PORT:3000}
    }
}
```

### Key Points

- `{$DOMAIN:localhost}` reads from env var, defaults to `localhost`.
  - `localhost` → Caddy serves HTTP only (no auto-HTTPS).
  - A real domain → Caddy automatically provisions TLS via Let's Encrypt.
- More specific `handle` blocks match first. The bare `handle` is the catch-all.
- Frontend container handles SPA routing internally (e.g., nginx `try_files`).
- No `uri strip_prefix` unless the backend doesn't expect the path prefix.

### Alternative: Caddy Serving Static Files Directly

If the frontend is not a separate container but static files copied into the Caddy image:

```caddyfile
{$DOMAIN:localhost} {
    encode gzip zstd

    handle /api/* {
        reverse_proxy backend:{$BACKEND_PORT:8000}
    }

    handle {
        root * /srv/www
        try_files {path} /index.html
        file_server
    }
}
```

---

## 5. Docker Compose

```yaml
services:
  backend:
    image: ${BACKEND_IMAGE}
    restart: unless-stopped
    expose:
      - "${BACKEND_PORT:-8000}"
    volumes:
      - app_data:/app/data
    healthcheck:
      test: ["CMD", "python", "-c", "import urllib.request; urllib.request.urlopen('http://localhost:${BACKEND_PORT:-8000}/health')"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 10s

  frontend:
    image: ${FRONTEND_IMAGE}
    restart: unless-stopped
    expose:
      - "${FRONTEND_PORT:-3000}"
    depends_on:
      backend:
        condition: service_healthy

  caddy:
    image: caddy:2-alpine
    restart: unless-stopped
    ports:
      - "${HTTP_PORT:-80}:80"
      - "${HTTPS_PORT:-443}:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    depends_on:
      backend:
        condition: service_healthy

volumes:
  app_data:
  caddy_data:
  caddy_config:
```

### Design Decisions

- `expose` (not `ports`) for backend/frontend — only Caddy is reachable from the host.
- `condition: service_healthy` gates startup on actual readiness, not just container start.
- `caddy_data` persists TLS certificates across restarts.
- `app_data` persists the database. Use a named volume (not a bind-mounted file) to avoid inode replacement issues with SQLite.

---

## 6. Environment Variables (.env)

```bash
# .env.example

# --- Images ---
BACKEND_IMAGE=ghcr.io/org/project-backend:latest
FRONTEND_IMAGE=ghcr.io/org/project-frontend:latest

# --- Caddy ---
DOMAIN=localhost          # Real domain enables auto-HTTPS
HTTP_PORT=80
HTTPS_PORT=443
```

### What Goes in .env

Only host-facing, deployment-specific values:
- Docker image references (registry, tag)
- Domain name
- Host-bound ports

### What Does NOT Go in .env

- Internal service hostnames (`backend`, `frontend`) — Docker DNS names, never change.
- Internal container ports (`8000`, `3000`) — fixed by Dockerfile.
- Database paths — fixed path inside container.
- Network names — unnecessary unless integrating with external stacks.

---

## 7. Frontend Docker Image

The frontend image should:
1. Build the SPA in a multi-stage build.
2. Serve the static output with a lightweight HTTP server.
3. Bake the server configuration into the image.

```dockerfile
# --- Build stage ---
FROM node:20-alpine AS build
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY . .
RUN pnpm run build

# --- Runtime stage ---
FROM nginx:alpine
COPY --from=build /app/dist /usr/share/nginx/html

# Bake nginx config into the image
RUN printf 'server {\n\
    listen 3000;\n\
    server_name _;\n\
    root /usr/share/nginx/html;\n\
    index index.html;\n\
    location / {\n\
        try_files $uri $uri/ /index.html;\n\
    }\n\
    location /healthz {\n\
        access_log off;\n\
        return 200 "{\\"ok\\":true}";\n\
        add_header Content-Type application/json;\n\
    }\n\
}' > /etc/nginx/conf.d/default.conf

EXPOSE 3000
CMD ["nginx", "-g", "daemon off;"]
```

### SPA API Base URL

Two approaches:

**Build-time injection (recommended for same-origin deployments):**
- Production: `VITE_API_BASE` defaults to empty string `""` (same-origin, routed by reverse proxy).
- Local dev: `VITE_API_BASE=http://localhost:8000` (direct cross-origin call).
- No runtime injection needed when the reverse proxy always provides same-origin access.

**Runtime injection (for multi-environment images):**
- Use an `entrypoint.sh` that writes a `config.json` or `window.__env__` script at container start.
- The app fetches/reads this before rendering.
- Only needed when the same image must serve different API URLs without rebuilding.

---

## 8. Backend Docker Image

The backend image should:
1. Use a slim base image.
2. Run with `--proxy-headers` and `--forwarded-allow-ips *` to trust the reverse proxy.
3. Include a `/health` endpoint for Docker healthchecks.

```dockerfile
FROM python:3.11-slim
WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY . .
RUN mkdir -p /app/data

EXPOSE 8000

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8000/health')" || exit 1

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000", "--proxy-headers", "--forwarded-allow-ips", "*"]
```

---

## 9. CORS Policy

| Scenario | CORS Needed? | Configuration |
|---|---|---|
| Production (same origin via proxy) | No | Browser sees one origin — no CORS headers required |
| Local dev (separate origins) | Yes | Backend allows `*` or the frontend dev origin |
| External API consumers (curl, mobile) | Yes | Backend allows `*` or specific origins |

Recommendation: keep backend CORS permissive (`allow_origins=["*"]`) for simplicity in trusted/local deployments. Tighten for public-facing APIs.

---

## 10. Healthcheck Patterns

| Service | Method | Endpoint |
|---|---|---|
| Backend (Python) | `python -c "import urllib.request; ..."` | `/health` |
| Backend (with curl) | `curl -f http://localhost:PORT/health` | `/health` |
| Frontend (nginx) | `wget -qO- http://localhost:PORT/healthz` | `/healthz` |
| Frontend (caddy) | `wget -qO- http://localhost:PORT/` | `/` |

Tuning:

| Parameter | Recommended | Notes |
|---|---|---|
| `interval` | 10–30s | How often to poll |
| `timeout` | 3–5s | Max time for one check |
| `retries` | 3–5 | Failures before `unhealthy` |
| `start_period` | 10–30s | Grace period during startup |

---

## 11. Checklist

Before deploying, verify:

- [ ] Frontend dev-server proxy is removed (no `server.proxy` in Vite config, no `proxy` in package.json).
- [ ] Frontend `API_BASE` env var defaults to `""` (same-origin) for production builds.
- [ ] Backend CORS is configured (at minimum `allow_origins=["*"]`).
- [ ] Backend runs with `--proxy-headers` in Docker.
- [ ] `docker-compose.yml` uses `expose` (not `ports`) for backend and frontend.
- [ ] Only the reverse proxy binds to host ports.
- [ ] Caddyfile routes API paths to backend, catch-all to frontend.
- [ ] Named volumes exist for database and Caddy TLS data.
- [ ] Healthchecks are defined for backend (and optionally frontend).
- [ ] `.env.example` documents all required variables.
- [ ] `DOMAIN` defaults to `localhost` for local Docker testing.
