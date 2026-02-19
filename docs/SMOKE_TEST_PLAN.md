# Smoke Test Plan: LLM Proxy Gateway

## Prerequisites

- Backend running at `http://localhost:8000`
- Frontend running at `http://localhost:5173`
- At least one model configured with active endpoints
- Database accessible with existing data

---

## ST-1: Backend Health Endpoint

**Objective**: Verify the gateway health endpoint responds correctly.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | `GET http://localhost:8000/health` | `200` with `{"status": "ok", "version": "0.1.0"}` |

---

## ST-2: Endpoint Health Check (Bug Fix Verification)

**Objective**: Verify health check uses real chat completion request with configured model ID, not probe endpoints that cause 404.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | `POST /api/endpoints/{id}/health-check` for an OpenAI endpoint | `200` with `health_status` = `healthy` and `detail` containing "Connection successful" or valid response info, NOT "HTTP 404" |
| 2 | `POST /api/endpoints/{id}/health-check` for an Anthropic endpoint | `200` with `health_status` = `healthy` and `detail` containing valid response info, NOT "HTTP 404" |
| 3 | Verify `response_time_ms` field is present and > 0 | Field exists and is a positive integer |
| 4 | Open frontend → Model Detail → Click Health Check on an endpoint | Toast shows "healthy" with valid detail, health dot turns green |
| 5 | Open frontend → Edit Endpoint dialog → Click "Test Connection" | Result banner shows green "healthy" status |

---

## ST-3: Proxy Request (Non-Streaming)

**Objective**: Verify proxy forwards requests correctly.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | `POST /v1/chat/completions` with `{"model": "{configured_openai_model}", "messages": [{"role": "user", "content": "Say hello"}], "max_tokens": 5}` | `200` with valid chat completion response |
| 2 | `POST /v1/messages` with `{"model": "{configured_anthropic_model}", "max_tokens": 5, "messages": [{"role": "user", "content": "Say hello"}]}` | `200` with valid Anthropic response |

---

## ST-4: Statistics - Request Logging

**Objective**: Verify proxy requests are automatically logged.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Make a proxy request (ST-3 step 1) | Request completes successfully |
| 2 | `GET /api/stats/requests?limit=1` | `200` with at least 1 item matching the request just made (model_id, status_code, response_time_ms > 0) |
| 3 | Verify log entry has `model_id`, `provider_type`, `status_code`, `response_time_ms`, `request_path`, `created_at` | All fields present and non-null |

---

## ST-5: Statistics - Aggregated Summary

**Objective**: Verify statistics summary endpoint returns correct aggregations.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | `GET /api/stats/summary` | `200` with `total_requests` >= 1, `success_rate` between 0-100, `avg_response_time_ms` > 0 |
| 2 | `GET /api/stats/summary?group_by=model` | `200` with `groups` array containing entries keyed by model_id |
| 3 | `GET /api/stats/summary?group_by=provider` | `200` with `groups` array containing entries keyed by provider_type |

---

## ST-6: Statistics - Filtering

**Objective**: Verify request log filtering works correctly.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | `GET /api/stats/requests?model_id={known_model}` | Only entries for that model returned |
| 2 | `GET /api/stats/requests?success=true` | Only entries with 2xx status codes |
| 3 | `GET /api/stats/requests?success=false` | Only entries with non-2xx status codes (may be empty) |
| 4 | `GET /api/stats/requests?limit=2&offset=0` | At most 2 items, `total` reflects actual count |

---

## ST-7: Statistics - Frontend UI

**Objective**: Verify the Statistics page renders correctly in the frontend.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Navigate to `http://localhost:5173/statistics` | Statistics page loads without errors |
| 2 | Verify sidebar shows "Statistics" nav link | Link is present and active when on the page |
| 3 | Verify overview cards display: Total Requests, Avg Response Time, Success Rate, Total Tokens | All 4 cards visible with numeric values |
| 4 | Verify request log table displays entries | Table shows rows with timestamp, model, provider, status, response time columns |
| 5 | Apply a model filter | Table updates to show only matching entries |
| 6 | Apply a time range preset (e.g., "Last 24h") | Table updates to show only entries within range |

---

## ST-8: Frontend Navigation

**Objective**: Verify all frontend routes work.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Navigate to `/dashboard` | Dashboard page loads with model overview |
| 2 | Navigate to `/models` | Models page loads with model list |
| 3 | Navigate to `/models/{id}` | Model detail page loads with endpoints |
| 4 | Navigate to `/statistics` | Statistics page loads with data |
| 5 | Verify sidebar navigation links work | All links navigate correctly |

---

## ST-9: CRUD Operations

**Objective**: Verify basic CRUD still works after changes.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | `GET /api/providers` | `200` with list of providers |
| 2 | `GET /api/models` | `200` with list of models |
| 3 | `GET /api/models/{id}` | `200` with model detail including endpoints |

---

## Execution Notes

- Tests should be run in order (ST-1 through ST-12)
- ST-3 must run before ST-4/ST-5/ST-6 to ensure log data exists
- ST-3 must run before ST-10 to ensure endpoint success rate data exists
- Frontend tests (ST-7, ST-8, ST-10, ST-11, ST-12) require both backend and frontend running
- Health check tests (ST-2) validate the specific bug fix (404 → real request)

---

## ST-10: Endpoint Success Rate Badge

**Objective**: Verify endpoint success rate badges display correctly on Model Detail page.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | `GET /api/stats/endpoint-success-rates` | `200` with array of objects, each having `endpoint_id`, `total_requests`, `success_count`, `error_count`, `success_rate` |
| 2 | Verify endpoints with requests have `success_rate` as a number (0-100) | `success_rate` is a float between 0 and 100 |
| 3 | Verify endpoints with no requests have `success_rate` as `null` | `success_rate` is null, `total_requests` is 0 |
| 4 | Open frontend → Model Detail page for a model with request data | Endpoint table shows success rate badge instead of health dot |
| 5 | Verify badge color: endpoint with ≥98% success rate shows green badge | Green badge with percentage |
| 6 | Verify badge color: endpoint with 75-98% success rate shows yellow badge | Yellow badge with percentage |
| 7 | Verify badge color: endpoint with <75% success rate shows red badge | Red badge with percentage |
| 8 | Verify badge for endpoint with no requests shows "N/A" gray badge | Gray "N/A" badge |
| 9 | Hover over success rate badge | Tooltip shows: success rate %, total requests, success/error counts, last health check detail |

---

## ST-11: Model Health Display

**Objective**: Verify model health badges display on Dashboard and Models pages.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | `GET /api/models` | Response includes `health_success_rate` and `health_total_requests` fields for each model |
| 2 | Verify model with request data has `health_success_rate` as a number | `health_success_rate` is a float between 0 and 100 |
| 3 | Verify model with no request data has `health_success_rate` as `null` | `health_success_rate` is null |
| 4 | Open frontend → Dashboard page | Model Overview table has a "Health" column |
| 5 | Verify health badge shows colored percentage for models with data | Badge shows percentage with appropriate color (green/yellow/red) |
| 6 | Verify health badge shows "N/A" for models with no data | Gray "N/A" badge |
| 7 | Open frontend → Models page | Model list table has a "Health" column |
| 8 | Verify health badges match Dashboard display | Same badge format and colors as Dashboard |

---

## ST-12: Statistics Provider Filter (Supported Providers Only)

**Objective**: Verify Statistics page only shows supported providers in the filter dropdown.

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Open frontend → Statistics page | Page loads without errors |
| 2 | Click the Provider Type dropdown | Dropdown opens |
| 3 | Verify dropdown options | Only shows: "All Providers", "OpenAI", "Anthropic", "Gemini" |
| 4 | Verify "Ollama" is NOT in the dropdown | Ollama option is absent |
| 5 | Verify "vLLM" is NOT in the dropdown | vLLM option is absent |
| 6 | Select "OpenAI" filter | Table filters to show only OpenAI requests |
| 7 | Select "All Providers" filter | Table shows all requests again |
