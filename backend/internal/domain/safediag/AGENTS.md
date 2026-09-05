# Safe Diagnostics

`matcher.go`, `scrub.go`, `extract.go`, `urls.go`, `metadata.go`, and `limits.go` own the shared credential-redaction boundary for runtime diagnostics, audit capture, and legacy backfill.

- Header Blocklist rules may add exact/prefix sensitive names but cannot weaken the fixed matcher. Preserve the non-secret exception and keep the frontend browser matcher aligned.
- Scrub before writing metadata, outbox/staging rows, PostgreSQL, logs, traces, or management diagnostics. Provider errors contribute only allowlisted scrubbed scalars, never raw error bodies.
- Truncate on UTF-8 code-point boundaries and retain the fixed byte/codepoint limits.
- Diagnostic redaction uses `[REDACTED]`; do not substitute secret hashes, prefixes, or lengths. The separate `CustomHeaderRedactedValue` sentinel is the write-only management header contract: submitting it preserves the stored header value.
- Credential redaction does not promise PII-free content or general body DLP.
