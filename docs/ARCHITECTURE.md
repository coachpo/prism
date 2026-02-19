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
│   │   ├── endpoint.py         # BaseURL + APIKey entries
│   │   └── request_log.py      # Request telemetry log entries
│   ├── schemas/                # Pydantic request/response schemas
│   │   ├── provider.py
│   │   ├── model_config.py
│   │   ├── endpoint.py
│   │   └── stats.py            # Statistics query/response schemas
│   ├── routers/                # API route handlers
│   │   ├── providers.py        # CRUD for provider types
│   │   ├── models.py           # CRUD for model configurations
│   │   ├── endpoints.py        # CRUD for BaseURL/APIKey combos
│   │   ├── proxy.py            # LLM proxy endpoints
│   │   └── stats.py            # Statistics query endpoints
│   ├── services/               # Business logic
│   │   ├── proxy_service.py    # Request forwarding, streaming
│   │   ├── loadbalancer.py     # LB strategy, failover
│   │   └── stats_service.py    # Request logging, aggregation queries
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
│   │   ├── EndpointConfig.tsx  # Endpoint management
│   │   └── StatisticsPage.tsx  # Request statistics & analytics
│   └── types/
│       └── api.ts              # TypeScript types matching backend schemas
├── components.json             # shadcn config
├── package.json
├── vite.config.ts
├── tsconfig.json
└── tailwind.config.ts
```

## 3. Request Flow

### 3.1 Proxy Request (Non-Streaming, Native Model)

```
Client → POST /v1/chat/completions {model: "gpt-4o"}
  → Router extracts model ID from body
  → LoadBalancer looks up model config
  → Model is native → select endpoint directly
  → ProxyService forwards request to upstream BaseURL
  → Upstream responds with JSON
  → Gateway returns JSON to client
```

### 3.2 Proxy Request (Proxy/Alias Model)

```
Client → POST /v1/messages {model: "claude-sonnet-4-5"}
  → Router extracts model ID from body
  → LoadBalancer looks up model config
  → Model is proxy → resolve redirect_to → "claude-sonnet-4-5-20250929"
  → Look up target native model config
  → Select endpoint from target model
  → ProxyService forwards request to upstream BaseURL (request body unchanged)
  → Upstream responds
  → Gateway returns response to client
```

### 3.3 Proxy Request (Streaming)

```
Client → POST /v1/chat/completions {model: "gpt-4o", stream: true}
  → Router extracts model ID
  → LoadBalancer selects endpoint (with proxy alias resolution if needed)
  → ProxyService opens streaming connection to upstream
  → SSE chunks piped directly to client via StreamingResponse
  → On upstream error: failover to next endpoint (if configured)
```

### 3.4 Provider-Specific Routing

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

## 5. Model Proxy (Alias)

### 5.1 Concept
Proxy models are aliases that forward requests to a target native model. This resolves model ID suffix variations (e.g., `claude-sonnet-4-5` → `claude-sonnet-4-5-20250929`).

### 5.2 Rules
- Only same-provider proxying (OpenAI → OpenAI, Anthropic → Anthropic)
- Target must be a native model (no chained proxy aliases)
- Proxy models have no endpoints of their own
- Proxy models do not use load balancing (lb_strategy is ignored; target model's strategy applies)
- All model IDs are globally unique
- The gateway does NOT modify the request body — it only uses the target model's endpoints for routing

### 5.3 Resolution
```
resolve_model(model_id):
  config = lookup(model_id)
  if config.model_type == "proxy":
    return lookup(config.redirect_to)
  return config
```

## 6. Endpoint Health Detection

### 6.1 Concept
Manual health checks allow users to verify endpoint connectivity and authentication before relying on them for proxy traffic.

### 6.2 Health Probes (Provider-Specific)
Health checks send a real chat completion request using the endpoint's configured model ID and a simple question. This validates the full request chain (URL routing, authentication, model availability) using the same URL-building logic as the proxy engine.

- **OpenAI/Gemini**: `POST {base_url}/chat/completions` with `{"model": "{model_id}", "max_tokens": 1, "messages": [{"role": "user", "content": "hi"}]}`
- **Anthropic**: `POST {base_url}/messages` with `{"model": "{model_id}", "max_tokens": 1, "messages": [{"role": "user", "content": "hi"}]}`

### 6.3 Status Values
- `unknown` — Never checked (default)
- `healthy` — Last check succeeded (2xx or 429)
- `unhealthy` — Last check failed (401/403, connection error, timeout, other errors)

## 7. Request Statistics

### 7.1 Concept
All proxy requests are automatically logged with telemetry data for analytics and debugging.

### 7.2 Logging Flow
```
Client → Proxy Router → LoadBalancer → ProxyService → Upstream
                                                         ↓
                                              Response received
                                                         ↓
                                              Log request to DB
                                              (non-blocking)
                                                         ↓
                                              Return response to client
```

### 7.3 Data Captured
- Model ID, provider type, endpoint used
- HTTP status code, response time (ms)
- Token usage (input, output, total) — extracted from upstream response
- Stream flag, request path, error details

### 7.4 Query Capabilities
- Filter by model, provider, status, time range
- Aggregated statistics with grouping by model/provider/endpoint
- Pagination for request log listing

## 8. Database Design

See [DATA_MODEL.md](./DATA_MODEL.md) for complete schema.

## 9. API Design

See [API_SPEC.md](./API_SPEC.md) for complete endpoint documentation.

## 10. Security Considerations

- No authentication (trusted local network assumption)
- API keys stored in plaintext in SQLite (acceptable for single-user local)
- CORS allows all origins (wildcard)
- No TLS termination (run behind reverse proxy for HTTPS if needed)
- SQLite file permissions should be restricted to owner
