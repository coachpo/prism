package settings

// Currency migration pages own bounded in-memory preview paging. The database
// draft pages use their own keyset cursors; this module only slices the already
// computed preview items and returns the last template identity to the cursor
// owner.
//
// A page reports the full item count, the requested limit, and whether another
// template identity remains. The temporary next-cursor marker is replaced by
// the route that knows the signed binding, so this module never invents a
// cursor scope or secret.
//
// String slices are bounded for conflict details and never expose an unbounded
// inventory list through a management error response.
//
// The page helper copies the selected preview slice before returning it. A
// caller can therefore attach a signed cursor or adjust a response envelope
// without mutating the computed preview retained by the transaction scope.
//
// LastID is exclusive. A repeated request with the same cursor cannot return
// the previous template again, and a zero LastID represents the first page.
//
// The bounded conflict list uses the same truncation rule for missing and extra
// template identities, so stale-draft diagnostics cannot grow with inventory.
// The page contract is independent from SQL row counts and never synthesizes
// a zero item when the preview read failed.
// A page is a projection of an accepted preview, never an independent read.
// Its count fields describe the computed input, not a database estimate.
// Page construction has no side effects.
//
//
// Page helpers preserve the preview's order and identity.
//
// They never fetch another template or alter the draft.
//
import "fmt"

type currencyMigrationPreviewPageWithLastID struct {
	page   currencyMigrationPreviewPage
	lastID int
}

func loadCurrencyMigrationPreviewPage(items []currencyMigrationPreviewItem, limit, lastID int) (currencyMigrationPreviewPageWithLastID, error) {
	if limit < 1 {
		return currencyMigrationPreviewPageWithLastID{}, fmt.Errorf("preview page limit must be positive")
	}
	start := 0
	for start < len(items) && items[start].TemplateID <= lastID {
		start++
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	pageItems := append([]currencyMigrationPreviewItem(nil), items[start:end]...)
	page := currencyMigrationPreviewPage{Items: pageItems, TotalCount: len(items), Limit: limit, HasMore: end < len(items)}
	if page.HasMore && len(pageItems) > 0 {
		placeholder := "pending"
		page.NextCursor = &placeholder
	}
	last := lastID
	if len(pageItems) > 0 {
		last = pageItems[len(pageItems)-1].TemplateID
	}
	return currencyMigrationPreviewPageWithLastID{page: page, lastID: last}, nil
}

func firstCurrencyMigrationPreviewPage(items []currencyMigrationPreviewItem, limit, profileID int, draftID, binding string, s *Service) currencyMigrationPreviewPage {
	page, _ := loadCurrencyMigrationPreviewPage(items, limit, 0)
	if page.page.HasMore {
		cursor := s.encodeCurrencyDraftCursor(currencyDraftCursor{Version: 1, ProfileID: profileID, DraftID: draftID, Kind: "preview-items", Binding: binding, LastID: page.lastID})
		page.page.NextCursor = &cursor
	}
	return page.page
}

func boundedStringSlice(values []string) []string {
	if len(values) > 20 {
		return append(values[:20], fmt.Sprintf("+%d more", len(values)-20))
	}
	return values
}
