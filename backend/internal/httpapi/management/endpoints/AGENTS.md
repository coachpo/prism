# Endpoint Guidance

Inline endpoint creation for Terminal Targets remains in `../connections/` and shares `../../../endpointdomain/` helpers.

- Use the canonical direct-reference query for summaries, filters, detail, and deletion. Disabled references and orphan connections still block deletion; recursive reachability is not reference truth. Recheck under lock, and preserve typed stale-cursor and in-use responses.
- Keep stored keys encrypted and read responses metadata-only. Derive fingerprints from plaintext with the endpoint-domain HMAC helper, never from ciphertext; `updated_at` is not key-rotation evidence.
- Increment `config_revision` only for normalized base-URL or key-identity changes. Name-only edits update `updated_at`; a fully unchanged write remains a no-op.
- Endpoint order is `lower(name), name, id`. Target ordering belongs to `model_access_targets.position`; do not restore endpoint positions or move routes.
- Verification uses family-specific metadata probes and the shared provider-auth scheme, with bounded response/error reads and same-origin redirects. Do not persist probe results or emit generation traffic, request history, usage, audit, or load-balance feedback.
