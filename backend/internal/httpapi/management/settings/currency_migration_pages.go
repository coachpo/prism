package settings

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
