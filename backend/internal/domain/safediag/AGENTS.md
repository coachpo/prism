# BACKEND DOMAIN SAFEDIAG KNOWLEDGE BASE

## OVERVIEW
`safediag/` owns the fixed-bottom-line safe diagnostic contract from Requests/Audit SPEC §4.2/§4.3/§5.5: immutable sensitive-name/value matching, provider JSON error envelope extraction, stable failure codes, URL and external-metadata scrubbing, and UTF-8-safe bounded truncation. It is HTTP-neutral and shared by runtime failure diagnostics, audit header/body capture, legacy backfill, and the frontend browser mask (which mirrors the same matcher).

## STRUCTURE
```text
safediag/
├── matcher.go       # Fixed sensitive-name exact/fragment rules + non-secret exception + extra exact/prefix rules
├── scrub.go         # Value scrubber: credentials, JWT/token-like fragments, key=value, control chars, whitespace fold
├── urls.go          # request_url / endpoint_base_url scrubbing with provenance
├── extract.go       # Provider JSON error envelope extraction + stable fallback codes
├── codes.go         # Stable error-code grammar and fallback helpers
├── metadata.go      # Schema-versioned external metadata-field enum, per-field caps, value scrubbing
├── limits.go        # Fixed byte/codepoint caps (4 KiB error detail, etc.)
└── *_test.go        # Table-driven unit tests
```

## CONVENTIONS
- All rules are code-fixed constants; the mutable Header Blocklist can only ADD sensitive names (exact/prefix), never weaken the fixed bottom line.
- Scrubbing is irreversible: pre-scrub values must never enter metadata, outbox, staging, DB, logs, traces, or management responses.
- Truncation MUST respect UTF-8 code-point boundaries; never byte-slice into a multi-byte character.
- The browser mask mirrors this matcher; keep the Go rules and the TS mirror in sync (same names, same fragments, same exception).

## ANTI-PATTERNS
- Do not treat the Header Blocklist as the only secret policy or as a body DLP.
- Do not return hashes, prefixes, or lengths as "safe replacement values"; replacement is always `[REDACTED]`.
- Do not persist raw provider error bodies into diagnostics; only allowlisted scalars after scrub.
- Do not declare diagnostics PII-free; scrub only guarantees credential redaction.
