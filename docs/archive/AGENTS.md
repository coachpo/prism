# DOCS ARCHIVE BOUNDARY

## OVERVIEW
`docs/archive/` is reserved for finished notes and retained evidence. It currently includes the sidecars live-smoke run note and screenshot alongside this boundary file; it is not a source-of-truth area, does not host active plans, and must not preserve stale implementation guidance as the only copy of a rule.

## WHAT BELONGS HERE
- Finalized run notes.
- Retained evidence tied to a completed investigation.
- Archived test records that follow `docs/archive/YYYY-MM-DD-llm-test-run-<scope>.md`.

## WHAT DOES NOT BELONG HERE
- Live architecture, API, or data-model docs.
- Active implementation plans.
- Canonical docs that still need editing.
- Upgrade guidance that should live in active root/backend/frontend/docs AGENTS files.

## CONVENTIONS
- Keep archive notes concise and dated.
- Use the run-note naming rule exactly: `docs/archive/YYYY-MM-DD-llm-test-run-<scope>.md`.
- Evidence assets such as screenshots may sit beside the dated run note they support.
- Prefer the parent docs or the owning backend or frontend AGENTS tree for live guidance.
- When archiving evidence, link back to the live owner instead of restating implementation contracts here.
