# DOCS ARCHIVE KNOWLEDGE BASE

## OVERVIEW
`docs/archive/` holds retained historical notes and evidence that are no longer the canonical source of truth. Active work stays in `../../.sisyphus/plans/`, and live contracts stay in `../ARCHITECTURE.md`, `../API_SPEC.md`, and `../DATA_MODEL.md`.

## STRUCTURE
```text
archive/
├── AGENTS.md
└── YYYY-MM-DD-*.md
```

## WHERE TO LOOK
- Live docs and current contracts: `../ARCHITECTURE.md`, `../API_SPEC.md`, `../DATA_MODEL.md`
- Supporting current references: `../PRD.md`, `../REQUESTS_PAGE.md`, `../SMOKE_TEST_PLAN.md`, `../TEST_CASE_GENERATION_METHODOLOGY.md`
- Active plans: `../../.sisyphus/plans/`

## CONVENTIONS
- Archive notes should use dated, descriptive filenames.
- Keep one markdown note as the anchor artifact; add adjacent `*.png` or `*.json` evidence only when the note needs provenance.
- Treat archived notes as historical context, not as current operating guidance.
- If an archived note contains a fact that becomes current contract again, move that fact back into the live docs instead of updating the archive note in place.

## ANTI-PATTERNS
- Do not store active implementation plans here.
- Do not treat archive notes as the source of truth when a live doc owns the topic.
- Do not leave standalone screenshots or payload dumps here without a parent markdown note.
