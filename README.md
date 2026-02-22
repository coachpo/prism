# Prism

**A lightweight, self-hosted LLM proxy gateway with intelligent routing, load balancing, and comprehensive observability.**

Prism acts as a unified gateway for multiple LLM API providers (OpenAI, Anthropic, Gemini), allowing you to configure, route, and load-balance requests through a single endpoint with a web-based management dashboard.

---

## Features

### Core Capabilities

- **Multi-Provider Support**: Route requests to OpenAI, Anthropic, and Gemini through a single `/v1/*` endpoint
- **Model Aliasing**: Create proxy models that resolve ID variations (e.g., `claude-sonnet-4-5` → `claude-sonnet-4-5-20250929`)
- **Load Balancing**: Single, round-robin, and failover strategies with automatic endpoint health tracking
- **Streaming Support**: Full support for SSE streaming responses with transparent pass-through

### Observability & Management

- **Request Telemetry**: Track latency, token usage, success rates, and error patterns
- **Audit Logging**: Optional per-provider request/response body capture with header redaction
- **Success Rate Badges**: Real-time endpoint health visualization based on 24h request data
- **Config Export/Import**: Full configuration backup and restore

### Architecture

- **Backend**: Python 3.11+ with FastAPI, async SQLAlchemy, aiosqlite
- **Frontend**: React 19 with TypeScript, Vite, TailwindCSS, shadcn/ui
- **Database**: SQLite (single-file, zero-config)
- **Deployment**: Docker images published to GHCR, or run locally with `./start.sh`

---

## Quick Start

### Prerequisites

- Python 3.11+
- Node.js 18+
- Git

### Local Development

```bash
# Clone the repository
git clone https://github.com/yourusername/prism.git
cd prism

# Initialize submodules
git submodule update --init --recursive

# Start full stack (backend + frontend)
./start.sh full

# Or backend only (headless)
./start.sh headless
```

The backend will be available at `http://localhost:8000` and the frontend at `http://localhost:5173`.

### Docker Compose (Recommended)

```bash
# Clone the repository
git clone --recursive https://github.com/coachpo/prism.git
cd prism

# Copy environment template
cp .env.example .env

# Start services
docker compose up -d

# View logs
docker compose logs -f

# Stop services
docker compose down
```

The backend will be available at `http://localhost:8000` and the frontend at `http://localhost:3000`.

### Docker (Manual)

```bash
# Pull images
docker pull ghcr.io/coachpo/prism-backend:latest
docker pull ghcr.io/coachpo/prism-frontend:latest

# Run backend
docker run -d \
  --name prism-backend \
  -p 8000:8000 \
  -v prism_data:/app/data \
  ghcr.io/coachpo/prism-backend:latest

# Run frontend
docker run -d \
  --name prism-frontend \
  -p 3000:3000 \
  ghcr.io/coachpo/prism-frontend:latest
```

The frontend image defaults to **same-origin API calls**. In production, put frontend and backend behind a reverse proxy and route:
- `/` to frontend (`:3000`)
- `/api`, `/v1`, `/v1beta` to backend (`:8000`)

---

## Usage

### 1. Configure Providers

Navigate to **Settings** and add API keys for your providers (OpenAI, Anthropic, Gemini).

### 2. Create Models

Go to **Models** → **Add Model**:

- **Native models**: Real models with their own endpoint configurations
- **Proxy models**: Aliases that forward to native models (for ID resolution)

### 3. Add Endpoints

For native models, add one or more endpoints:

- Base URL (e.g., `https://api.openai.com/v1`)
- API Key
- Priority (lower = tried first)
- Optional custom headers

### 4. Route Requests

Send requests to Prism's `/v1/*` endpoint using any OpenAI-compatible client:

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4-turbo",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

Prism will:

1. Resolve the model (handle aliases if it's a proxy)
2. Select an endpoint based on load balancing strategy
3. Forward the request with the correct provider auth headers
4. Log telemetry and audit data (if enabled)

---

## Project Structure

```
prism/
├── backend/          # FastAPI API + proxy engine (git submodule)
├── frontend/         # React SPA dashboard (git submodule)
├── docs/             # Architecture, API spec, data model, PRD, deployment
├── .github/workflows # CI/CD: Docker builds + cleanup
├── start.sh          # Unified dev launcher
└── AGENTS.md         # Project knowledge base for AI assistants
```

---

## Documentation

- [Architecture](docs/ARCHITECTURE.md) - Request flows, design decisions, provider routing
- [API Specification](docs/API_SPEC.md) - Full REST API documentation
- [Data Model](docs/DATA_MODEL.md) - Database schema reference
- [Deployment Guide](docs/DEPLOYMENT.md) - Production deployment instructions
- [PRD](docs/PRD.md) - Product requirements and feature specifications
- [Smoke Test Plan](docs/SMOKE_TEST_PLAN.md) - Comprehensive test scenarios

---

## Development

### Backend

```bash
cd backend

# Create virtual environment
python -m venv venv
source venv/bin/activate  # or `venv\Scripts\activate` on Windows

# Install dependencies
pip install -r requirements.txt

# Run server
uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload

# Run tests
pytest tests/
```

See [backend/README.md](backend/README.md) for more details.

### Frontend

```bash
cd frontend

# Install dependencies
pnpm install

# Run dev server
pnpm run dev

# Build for production
pnpm run build

# Lint
pnpm run lint
```

See [frontend/README.md](frontend/README.md) for more details.

---

## Configuration

### Environment Variables

**Backend:**

- `BACKEND_PORT` - Server port (default: 8000)
- `DATABASE_URL` - SQLite database path (default: `gateway.db`)

**Frontend:**

- `VITE_API_BASE` - Optional backend API base URL (default: same-origin `""`)
- `FRONTEND_PORT` - Dev server port (default: 5173)

When `VITE_API_BASE` is unset, local `./start.sh full` development still works because Vite proxies `/api`, `/v1`, and `/v1beta` to the backend.

### Database

Prism uses SQLite with automatic schema migrations. The database file is created on first run at `backend/gateway.db`.

---

## Security Considerations

Prism is designed for **trusted local/LAN deployments**:

- No authentication layer (wildcard CORS)
- API keys stored in plaintext in SQLite
- No rate limiting or abuse protection

**Do not expose Prism directly to the public internet.** Use a reverse proxy with authentication if remote access is needed.

---

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Submit a pull request

---

## License

MIT License - see [LICENSE](LICENSE) for details.

---

## Why "Prism"?

A prism splits light into different wavelengths, just like Prism routes requests to different LLM providers. The name reflects the project's focus on transparency, visibility, and intelligent routing.
