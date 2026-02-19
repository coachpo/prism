# Architecture Document: LLM Proxy Gateway

## 1. System Overview

```
┌─────────────┐     ┌──────────────────────────────────────┐     ┌──────────────┐
│             │     │         LLM Proxy Gateway             │     │   Providers  │
│   Web UI    │────▶│                                      │────▶│  OpenAI API  │
│  (React)    │     │  ┌─────────┐  ┌──────────┐          │     │ Anthropic API│
│  Port 5173  │◀────│  │ Config  │  │  Proxy   │          │◀────│  Gemini API  │
│             │     │  │  API    │  │  Engine  │          │     │              │
└─────────────┘     │  └────┬────┘  └────┬─────┘          │     └──────────────┘
                    │       │            │                 │
                    │  ┌────▼────────────▼─────┐          │
                    │  │     SQLite Database    │          │
                    │  │  (models, endpoints,   │          │
                    │  │   lb_config)           │          │
                    │  └───────────────────────┘          │
                    │              Port 8000               │
                    └──────────────────────────────────────┘
```

## 2. Component Architecture

### 2.1 Backend (FastAPI)

```
backend/
├── app/
│   ├── main.py                 # App factory, lifespan, CORS, router mounting
│   ├── core/
│   │   ├── config.py           # App settings (pydantic-settings)
│   │   └── database.py         # Async engine, session factory, Base
│   ├── models/                 # SQLAlchemy ORM models
│   │   ├── provider.py         # Provider model (openai, anthropic, gemini)
│   │   ├── model_config.py     # Model ID → provider mapping
│   │   └── endpoint.py         # BaseURL + APIKey entries
│   ├── schemas/                # Pydantic request/response schemas
│   │   ├── provider.py
│   │   ├── model_config.py
│   │   └── endpoint.py
│   ├── routers/                # API route handlers
│   │   ├── providers.py        # CRUD for provider types
│   │   ├── models.py           # CRUD for model configurations
│   │   ├── endpoints.py        # CRUD for BaseURL/APIKey combos
│   │   └── proxy.py            # LLM proxy endpoints
│   ├── services/               # Business logic
│   │   ├── proxy_service.py    # Request forwarding, streaming
│   │   └── loadbalancer.py     # LB strategy, failover, health tracking
│   └── dependencies.py         # Shared FastAPI dependencies
├── requirements.txt
└── alembic/ (future)
```

### 2.2 Frontend (React + Vite)

```
frontend/
├── src/
│   ├── App.tsx                 # Root with router
│   ├── main.tsx                # Entry point
│   ├── lib/
│   │   ├── api.ts              # API client (fetch wrapper)
│   │   └── utils.ts            # Utility functions
│   ├── components/
│   │   └── ui/                 # shadcn/ui components
│   ├── pages/
│   │   ├── Dashboard.tsx       # Overview of all models
│   │   ├── ModelConfig.tsx     # Model configuration page
│   │   └── EndpointConfig.tsx  # Endpoint management
│   └── types/
│       └── api.ts              # TypeScript types matching backend schemas
├── components.json             # shadcn config
├── package.json
├── vite.config.ts
├── tsconfig.json
└── tailwind.config.ts
```

## 3. Request Flow

### 3.1 Proxy Request (Non-Streaming)

```
Client → POST /v1/chat/completions {model: "gpt-4o"}
  → Router extracts model ID from body
  → LoadBalancer selects endpoint for model
  → ProxyService forwards request to upstream BaseURL
  → Upstream responds with JSON
  → Gateway returns JSON to client
```

### 3.2 Proxy Request (Streaming)

```
Client → POST /v1/chat/completions {model: "gpt-4o", stream: true}
  → Router extracts model ID
  → LoadBalancer selects endpoint
  → ProxyService opens streaming connection to upstream
  → SSE chunks piped directly to client via StreamingResponse
  → On upstream error: failover to next endpoint (if configured)
```

### 3.3 Provider-Specific Routing

| Provider | Proxy Path | Upstream Path | Auth Header |
|---|---|---|---|
| OpenAI | `POST /v1/chat/completions` | `{base_url}/v1/chat/completions` | `Authorization: Bearer {key}` |
| Anthropic | `POST /v1/messages` | `{base_url}/v1/messages` | `x-api-key: {key}` + `anthropic-version: 2023-06-01` |
| Gemini | `POST /v1/chat/completions` | `{base_url}/v1/chat/completions` | `Authorization: Bearer {key}` |

Note: Gemini's OpenAI-compatible endpoint is used for simplicity.

## 4. Load Balancing

### 4.1 Strategies

- **single**: Use the one active endpoint (default)
- **round_robin**: Rotate across all active endpoints
- **failover**: Try primary, fall back to secondary on failure

### 4.2 Failure Detection

Failures that trigger failover:
- HTTP 429 (rate limited)
- HTTP 500, 502, 503, 529 (server errors)
- Connection timeout (> 10s connect, > 120s read)
- Connection refused / DNS failure

### 4.3 Health Tracking

Per-endpoint counters stored in memory (reset on restart):
- `success_count`: Successful requests
- `failure_count`: Failed requests
- `last_failure_at`: Timestamp of last failure
- `is_healthy`: Derived from recent failure rate

## 5. Database Design

See [DATA_MODEL.md](./DATA_MODEL.md) for complete schema.

## 6. API Design

See [API_SPEC.md](./API_SPEC.md) for complete endpoint documentation.

## 7. Security Considerations

- No authentication (trusted local network assumption)
- API keys stored in plaintext in SQLite (acceptable for single-user local)
- CORS allows all origins (wildcard)
- No TLS termination (run behind reverse proxy for HTTPS if needed)
- SQLite file permissions should be restricted to owner
