# Configuration Rule Guidance

Keep header-blocklist and User-Agent Client Rule HTTP boundaries in their respective `*_routes.go`, normalization in `*_validation.go`, and persistence in `*_store.go`.

- System header-blocklist rows may change only `enabled`; reject rename, pattern/match-type changes, and deletion. The list may include system rows alongside the effective profile's rows.
- Normalize header patterns before duplicate checks. Match types are `exact|prefix`; prefix patterns must end in `-`, and both create and the merged update must pass the same shape validation.
- User-Agent Client Rules own caller attribution. Keep them separate from header-blocklist behavior and startup bootstrap settings.
