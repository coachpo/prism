# BACKEND TEST BOUNDARY

## OVERVIEW
`backend/tests/` is Prism's top-level backend regression surface. It holds contract-heavy checks around runtime behavior, management APIs, and seed and backfill flows.

## COVERAGE CLUSTERS
- Config import and export timeout contracts.
- Request-log token-rate and TTFT contracts.
- Loadbalance defaults and summary coverage.
- Realtime connection manager coverage.
- Proxy timeout and runtime behavior.
- Usage-event persistence and backfill.
- Startup seeding.
- Pricing-template policy removal.
- User-agent seeding.

## WHERE TO LOOK
- Config and profile-config contract seams: `test_config_import_timeout_contract.py`, `test_profile_config_v3_contract.py`
- Proxy timeout and runtime seams: `test_proxy_transport_timeout.py`, `test_proxy_streaming_timeout_runtime.py`, `test_static_timeout_runtime.py`
- Realtime and loadbalance seams: `test_realtime_connection_manager.py`, `test_loadbalance_strategy_defaults_endpoint.py`, `test_loadbalance_strategy_summary.py`
- Usage-event persistence, spending, and backfill seams: `test_usage_event_*`, `test_startup_usage_event_backfill.py`, `test_spending_report_usage_event_source.py`

## CONVENTIONS
- Keep this doc at the test-tree root, not the leaf level.
- Do not invent child test AGENTS files.
- Keep regression notes grounded in current filenames and live backend ownership docs.
