package contracttest

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// The Pricing page reads its reference counts from the bounded keyset page, so
// the page has to carry rows on the very first request — a page that answers
// with a real total_count and an empty item list reads on screen as "reference
// information unavailable" for every row.

func TestPricingTemplateListPageWalksFromTheFirstPage(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertContractPricingTemplate(t, harness, profileID, "Page Template A")
	insertContractPricingTemplate(t, harness, profileID, "Page Template B")

	// No cursor: the first page carries the rows, not just the count.
	first := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/pricing-templates?limit=100", nil, http.StatusOK)
	if total := intValue(first["total_count"]); total != 2 {
		t.Fatalf("expected total_count 2, got %d", total)
	}
	if names := pricingPageNames(t, first); len(names) != 2 || names[0] != "Page Template A" || names[1] != "Page Template B" {
		t.Fatalf("expected both templates on the first page in name order, got %+v", names)
	}
	for _, raw := range first["items"].([]any) {
		if item := asMap(t, raw); asMap(t, item["current_revision"])["tier"] != nil {
			t.Fatalf("expected unconfigured page revision tier to be null, got %+v", item)
		}
	}
	if first["next_cursor"] != nil {
		t.Fatalf("expected no next cursor when the page covers every template, got %+v", first["next_cursor"])
	}

	// Cursor walk: each page carries its own rows and the walk terminates.
	pageOne := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/pricing-templates?limit=1", nil, http.StatusOK)
	if names := pricingPageNames(t, pageOne); len(names) != 1 || names[0] != "Page Template A" {
		t.Fatalf("expected the first keyset page to carry template A, got %+v", names)
	}
	cursor, ok := pageOne["next_cursor"].(string)
	if !ok || cursor == "" {
		t.Fatalf("expected a next cursor with templates remaining, got %+v", pageOne["next_cursor"])
	}
	pageTwo := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, fmt.Sprintf("/api/pricing-templates?limit=1&cursor=%s", url.QueryEscape(cursor)), nil, http.StatusOK)
	if names := pricingPageNames(t, pageTwo); len(names) != 1 || names[0] != "Page Template B" {
		t.Fatalf("expected the second keyset page to carry template B, got %+v", names)
	}
	if pageTwo["next_cursor"] != nil {
		t.Fatalf("expected the walk to end after the last template, got %+v", pageTwo["next_cursor"])
	}
	if consumed := intValue(pageTwo["consumed_count"]); consumed != 2 {
		t.Fatalf("expected consumed_count 2 after the full walk, got %d", consumed)
	}
}

// The bound is a contract callers have to stay inside; it is stated here so a
// client asking for more than one page's worth fails loudly in tests rather
// than degrading a live page to an unexplained read failure.
func TestPricingTemplateListPageLimitBounds(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertContractPricingTemplate(t, harness, profileID, "Bounds Template")

	for _, limit := range []string{"0", "101", "200", "abc"} {
		response := modelResponse(t, harness, profileID, http.MethodGet, "/api/pricing-templates?limit="+limit, nil)
		assertErrorResponse(t, response, http.StatusUnprocessableEntity, "limit must be between 1 and 100")
	}

	for _, limit := range []string{"1", "100"} {
		modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/pricing-templates?limit="+limit, nil, http.StatusOK)
	}
}

func pricingPageNames(t *testing.T, page map[string]any) []string {
	t.Helper()
	items, ok := page["items"].([]any)
	if !ok {
		t.Fatalf("expected an items array, got %+v", page["items"])
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, asMap(t, item)["name"].(string))
	}
	return names
}
