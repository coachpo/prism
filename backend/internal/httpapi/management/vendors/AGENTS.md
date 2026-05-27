# BACKEND MANAGEMENT VENDORS KNOWLEDGE BASE

## OVERVIEW
`management/vendors/` owns global vendor catalog CRUD under `/api/vendors*`. Vendors provide presentation metadata, audit preferences, and model usage lookup; runtime compatibility still comes from model `api_family`.

## STRUCTURE
```text
vendors/
├── service.go    # Service construction and vendor route mounting
├── routes.go     # Vendor CRUD and vendor-model listing
├── store.go      # Vendor persistence and model usage SQL
└── types.go      # Vendor request and response shapes
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Vendor list/get/create/update/delete: `routes.go`.
- `/vendors/{vendor_id}/models` usage listing: `routes.go`, `store.go`.
- Readonly vendor key protections: `routes.go`, `vendordomain`.

## CONVENTIONS
- Keep vendors global, not selected-profile scoped.
- Don't treat vendor metadata as runtime compatibility.
- Don't mutate readonly system vendor identity fields here.
- Keep vendor catalog export/import in `configbundle/`.

## LLM UPSTREAM MATRIX
- Vendor metadata changes must not imply runtime support for OpenAI, Anthropic, Gemini, or other upstream operation families.

## ANTI-PATTERNS
- Do not scope vendor catalog CRUD by selected profile.
- Do not use vendor rows or `icon_key` as runtime compatibility signals.
- Do not mutate readonly system vendor identity fields through CRUD routes.
- Do not move vendor catalog import/export out of `configbundle/`.
