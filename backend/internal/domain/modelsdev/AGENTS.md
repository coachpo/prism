# models.dev Domain

`client.go` owns catalog transport, `catalog.go` schema/numeric parsing, `match.go` exact matching and bounded search, and `pricing.go` fail-closed pricing plans. This boundary serves management and must not affect runtime routing or capability checks.

- `Fetch` is the network entrypoint: preserve HTTPS, pinned-origin redirects, whole-request timeout, body cap, ETag/304 reuse, and singleflight. Fetch before transactions; `Snapshot` and `CurrentRevision` are memory-only and may be used under a transaction.
- Parse prices losslessly through `json.Number`; explicit zero is configured data. Invalid schemas remain unavailable instead of yielding partial catalogs.
- Auto-binding requires one exact canonical-provider match. Do not auto-select aggregator providers outside that mapping or turn ambiguity into an arbitrary first match.
- Keep `BuildPricePlan` non-committable for unsupported, incomplete, non-USD, conflicting-tier, audio, or specialty-mismatched evidence. `CatalogPriceCurrency` declares USD; no currency conversion belongs here.
- Update the matching local client, schema, matching, or pricing tests; fixtures live in `modelsdev_testdata_test.go` and must remain independent of the live catalog.
