package settings

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
