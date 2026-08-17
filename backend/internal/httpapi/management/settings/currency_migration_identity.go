package settings

// Currency migration identity defines every hash whose input is part of a
// draft, chunk, inventory, or ledger identity. Canonical ordering happens
// before hashing so chunk upload order cannot alter the sealed draft hash.
//
// Header identity covers the normalized migration request. Draft identity
// covers the complete template set. Chunk identity covers one normalized
// payload. Ledger identity reads the expected inventory binding. The helpers
// are pure values: they do not read clocks, profiles, or database state.
//
// Stable field order in the JSON preimages is intentional. A change to any
// preimage changes replay, stale-preview detection, or audit evidence and must
// preserve the sorted-template identity rule.
// be treated as a contract change rather than a formatting change.
//
// The ledger hash is calculated from sorted template identities, not from the
// order in which chunks arrived. A replay therefore sees the same identity
// after a network retry or a different upload batching choice.
//
// Inventory identity remains separate from preview identity. This prevents a
// current FX inventory binding from being mistaken for a fresh template
// preview and keeps archive-only operations outside active-epoch cutover.
//
// Hash inputs are kept in the owning module so a route cannot accidentally
// hash a response DTO whose field order is allowed to evolve.
// Replay identity is a persistence contract, not a display convenience.
// It is the boundary that makes retry responses deterministic.
// Hash code remains separate from cursor signing and page slicing.
//
//
// Hashes are opaque to clients but durable for replay and audit evidence.
//
// Keep canonical preimages close to the functions that consume them.
//
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func hashCanonicalCurrencyDraft(headerHash string, items []currencyMigrationDraftItem) string {
	canonical := append([]currencyMigrationDraftItem(nil), items...)
	// Items are already ordered by the authoritative template query, but the
	// explicit sort makes the hash independent of chunk order.
	sortCurrencyDraftItems(canonical)
	raw, _ := json.Marshal(struct {
		HeaderHash string                       `json:"header_hash"`
		Items      []currencyMigrationDraftItem `json:"items"`
	}{headerHash, canonical})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func currencyDraftItemsHash(items []currencyMigrationDraftItem) string {
	canonical := append([]currencyMigrationDraftItem(nil), items...)
	sortCurrencyDraftItems(canonical)
	raw, _ := json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func sortCurrencyDraftItems(items []currencyMigrationDraftItem) {
	slicesSort(items, func(left, right currencyMigrationDraftItem) bool { return left.TemplateID < right.TemplateID })
}

func slicesSort[T any](items []T, less func(T, T) bool) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && less(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func inventoryHashForLedger(header currencyMigrationDraftHeaderRow) string {
	if header.ExpectedInventoryHash != nil {
		return *header.ExpectedInventoryHash
	}
	return ""
}
