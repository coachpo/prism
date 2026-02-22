# Deployment Guide: Prism

## Prerequisites

- Python 3.11+ (backend)
- Node.js 18+ (frontend)
- pnpm 10.30.1 (frontend package manager)

## Quick Start

### 1. Backend Setup

```bash
cd backend

# Create virtual environment
python3 -m venv venv
source venv/bin/activate  # macOS/Linux

# Install dependencies
pip install -r requirements.txt

# Run the server
uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload
```

The backend will:
- Auto-create `gateway.db` SQLite database on first run
- Seed default provider types (OpenAI, Anthropic, Gemini)
- Serve API at `http://localhost:8000`
- Serve OpenAPI docs at `http://localhost:8000/docs`

### 2. Frontend Setup

```bash
cd frontend

# Install dependencies
pnpm install

# Run development server
pnpm run dev
```

The frontend will be available at `http://localhost:5173`.

### 3. Configure Your First Model

1. Open `http://localhost:5173` in your browser
2. Navigate to "Models" in the sidebar
3. Click "Add Model"
4. Select a provider (e.g., OpenAI)
5. Enter the model ID (e.g., `gpt-4o`)
6. Add an endpoint with your BaseURL and API key
7. Save the configuration

### 4. Test the Proxy

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `sqlite+aiosqlite:///./data/gateway.db` | SQLite database path |
| `HOST` | `0.0.0.0` | Server bind address |
| `PORT` | `8000` | Server port |
| `LOG_LEVEL` | `info` | Logging level |
| `VITE_API_BASE` | *(unset, same-origin)* | Optional frontend API base URL override |

### CORS

CORS is configured to allow all origins (`*`) by default. This is suitable for local/LAN deployment. For production behind a reverse proxy, consider restricting origins.

## Project Structure

```
prism/
├── docs/                   # Documentation
│   ├── PRD.md
│   ├── ARCHITECTURE.md
│   ├── API_SPEC.md
│   ├── DATA_MODEL.md
│   └── DEPLOYMENT.md
├── backend/                # FastAPI backend
│   ├── app/
│   │   ├── main.py
│   │   ├── core/
│   │   ├── models/
│   │   ├── schemas/
│   │   ├── routers/
│   │   ├── services/
│   │   └── dependencies.py
│   └── requirements.txt
└── frontend/               # React frontend
    ├── src/
    │   ├── components/
    │   ├── pages/
    │   ├── lib/
    │   └── types/
    ├── package.json
    └── vite.config.ts
```
