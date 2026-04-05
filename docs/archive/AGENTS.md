# DOCS ARCHIVE KNOWLEDGE BASE

## OVERVIEW
`docs/archive/` holds finished implementation notes, executed test run records, and retained evidence files. It is archival support material, not the source of truth for active behavior or active planning.

## STRUCTURE
```text
docs/archive/
├── AGENTS.md
└── YYYY-MM-DD-*.md with optional adjacent evidence files
```

## WHERE TO LOOK
- Live source-of-truth docs: `../ARCHITECTURE.md`, `../API_SPEC.md`, `../DATA_MODEL.md`
- Parent docs guidance: `../AGENTS.md`
- Test-run archive policy: `../TEST_CASE_GENERATION_METHODOLOGY.md`
- Active working plans outside docs: `../../.sisyphus/plans/`

## CONVENTIONS
- Prefer dated descriptive markdown filenames.
- Keep one markdown note as the anchor before adding screenshots, payload JSON, or other evidence files.
- Archive only finished material; active work stays outside `docs/`.
- Summarize why the artifact matters and point back to the live owning doc or code path.

## ANTI-PATTERNS
- Do not treat archive notes as the source of truth when a live doc or AGENTS file owns the contract.
- Do not store active plans here.
- Do not add unattached binary or JSON evidence without a markdown note that explains it.
