# Prism Test Case Generation Methodology

## 1. Purpose

This document defines how an LLM agent should explore Prism end to end, generate test cases, execute them with browser tooling when needed, and preserve the executed cases for future regression work. It is a supporting reference and must stay grounded in live code, the normative docs, and the existing regression suite shape.

## 2. Source of Truth Order

When generating test cases, use these sources in order:

1. `docs/API_SPEC.md` for management, runtime, auth, and realtime contracts.
2. `docs/ARCHITECTURE.md` for route and component boundaries.
3. `docs/PRD.md` for intended operator workflows and product expectations.
4. `docs/SMOKE_TEST_PLAN.md` for existing manual smoke coverage.
5. `frontend/src/App.tsx` for the mounted route surface.
6. the live Go backend surface for the mounted backend router surface and `/health`.
7. Child AGENTS files under `frontend/` and `backend/` for local ownership boundaries inside the monorepo.
8. Current repo-owned backend and frontend documentation for the live implementation surface. The checked-in regression trees remain present under `backend/tests/` and `frontend/tests/`; when grounding backend coverage, prefer the Go runtime under `backend/cmd/`, `backend/internal/`, `backend/migrations/`, and the Go regression packages under `backend/tests/{contract,integration,runtime}`.

Do not generate cases for behavior that is not supported by those sources.

## 3. Prism-Specific Test Surfaces

The agent should always inventory Prism as these surfaces:

- Public auth routes: `/login`, `/forgot-password`, `/reset-password`
- Protected management routes: `/dashboard`, `/models`, `/models/:id`, `/models/:id/proxy`, `/endpoints`, `/loadbalance-strategies`, `/statistics`, `/settings`, `/proxy-api-keys`, `/pricing-templates`, `/request-logs`
- Management APIs on `/api/*`
- Runtime proxy APIs on `/v1/*` and `/v1beta/*`
- Realtime dashboard updates on `/api/realtime/ws`
- The selected-profile versus active-profile split
- Dense frontend management surfaces with forms, tables, dialogs, drawers, charts, and toasts

High-value frontend surfaces include the dashboard, models list, model detail, endpoints, loadbalance strategies, statistics, settings, proxy API keys, pricing templates, and request-log investigation flow. High-value backend surfaces include auth, profile lifecycle, config import or export, runtime proxy routing, failover, realtime dashboard updates, costing, audit logging, and observability queries.

## 4. Coverage Classes

### 4.1 Smoke coverage

Smoke cases should prove the system is viable, not exhaustively validated. Each smoke batch should cover:

- launcher and stack availability
- `/health`
- auth bootstrap and one login-protected route
- one management CRUD flow
- one runtime request per supported API family under test
- one realtime connection or subscription check
- one profile activation or scope check
- one config export or import sanity check

### 4.2 Functional coverage

Functional cases should exercise detailed contracts and boundary conditions, including:

- required fields, invalid body shapes, and invalid query parameters
- auth and authorization boundaries
- profile scoping rules and active-runtime routing rules
- proxy/native model invariants
- loadbalance, failover, and health-check behavior
- request-log, audit, costing, and statistics contracts
- config import validation and dependency checks

### 4.3 UI or UX coverage

UI or UX cases should focus on user-visible correctness in real layouts:

- route navigation and page shell stability
- dialogs, drawers, tables, cards, charts, and filter bars
- loading, empty, success, and error states
- focus flow, button affordances, visible status, and toast feedback

### 4.4 Long-text and localization coverage

Always include seeds with:

- long model IDs
- long vendor and endpoint names
- long emails and usernames
- long error strings
- long unbroken strings
- localized Chinese strings where locale-aware copy already exists

These cases exist to catch clipping, overlap, stacking problems, and truncation failures.

### 4.5 Illegal-input coverage

Always include invalid or conflicting inputs for:

- model routing rules
- loadbalance status codes and numeric ranges
- pricing decimal fields
- FX mappings and timezone settings
- auth password limits
- config import payload shape and reference validation
- request-log and statistics filters

`frontend/src/pages/models/modelFormState.ts`, `frontend/src/pages/loadbalance-strategies/loadbalanceStrategyFormState.ts`, `frontend/src/pages/pricing-templates/pricingTemplateFormState.ts`, `frontend/src/pages/settings/settingsPageHelpers.ts`, and `frontend/src/lib/configImportValidation.ts` are primary frontend seams for illegal-input case generation.

## 5. Generation Workflow

### Step 1: Build the inventory

Enumerate routes, APIs, realtime paths, and major state owners from the source-of-truth files.

### Step 2: Map the contract

For each surface, identify:

- the user or API entry point
- the expected success behavior
- the empty or loading behavior
- the error behavior
- the invalid-input behavior
- the visual-stress behavior

### Step 3: Produce case cards

Every generated case must include:

- case ID
- surface
- lane: smoke, functional, UI, long-text, or illegal-input
- preconditions or fixture needs
- exact action steps
- deterministic expected result
- visual expected result when applicable
- evidence to capture
- likely regression destination

### Step 4: Choose the execution tool

- Use **Playwright MCP** as the default browser tool for navigation, clicks, fills, screenshots, viewport changes, and HTTP calls.
- Use **Chrome DevTools MCP** when you need DOM snapshots, console output, network inspection, accessibility-tree text, or deeper layout diagnosis.
- Use both when a visual issue needs deterministic browser interaction plus low-level inspection.

### Step 5: Execute with an assertion ladder

Always validate in this order:

1. deterministic contract checks such as status codes, visible text, roles, labels, state changes, URL changes, or response payloads
2. screenshots for the final state under review
3. LLM visual review of the screenshot for overlap, clipping, disorder, or broken spacing

LLM image review must never replace deterministic contract checks.

### Step 6: Preserve the result

Every executed case must be saved for future regression use, even when it passes. The run record must include the exact case, the observed result, and the evidence bundle.

## 6. Case Record Format

Each executed case should be recorded with this shape:

```md
### CASE_ID: Short title
- Lane: Smoke | Functional | UI | LongText | IllegalInput
- Surface: frontend | backend | cross-stack
- Tooling: Playwright MCP | Chrome DevTools MCP | both
- Preconditions:
- Steps:
- Deterministic expected result:
- Visual expected result:
- Observed result:
- Evidence:
  - screenshot paths
  - network or console notes
  - response snippets when relevant
- Regression value: low | medium | high
- Promotion target:
```

## 7. Regression Preservation Rules

Executed cases must be preserved in two forms:

1. **Run record:** save the executed cases to `docs/archive/YYYY-MM-DD-llm-test-run-<scope>.md`.
2. **Promotion target:** assign each stable case to the place where it should become a future regression.

Promotion targets must follow Prism's current checked-in structure:

- Manual or browser-only smoke coverage that is not yet automated -> `docs/SMOKE_TEST_PLAN.md`
- Archive run notes -> `docs/archive/YYYY-MM-DD-llm-test-run-<scope>.md`
- Future automated backend or frontend regressions -> define a fresh owner-aligned destination adjacent to the live implementation surface before checking the case in

If a case is valuable but not yet ready for automation, it must still be preserved in the archive run note with a clear promotion target.

## 8. Prism-Specific Guardrails

- Keep selected-profile management scope separate from active runtime routing in every case set.
- Keep management auth and runtime proxy-key auth separate.
- Do not assume unsupported providers or routes.
- Prefer existing regression destinations over inventing a new suite shape.
- Save evidence and executed cases even when the outcome is "works as expected".
