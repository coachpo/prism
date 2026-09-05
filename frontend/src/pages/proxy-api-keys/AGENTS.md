# Proxy-key presentation

- Runtime proxy keys are global instance credentials. Widgets receive ledger/mutation state from `../../features/proxy-keys/`; the secret-session lifecycle is owned there, independently of these dialogs and query refreshes.
- Rotation and deletion use explicit confirmation dialogs. Mutation responses reconcile the authoritative item/capacity; do not reload the page or infer capacity locally.
- `ProxyKeyExpiryField.tsx` edits wall-clock time in Settings timezone and submits RFC3339. Preserve create-never and edit preserve/set/clear semantics; block DST gaps and disclose the earlier occurrence used for overlaps.
- The ledger usage column reads only the visible page, so it is not sortable. Failed usage is a visible read failure/stale state, never zero or blank success.
