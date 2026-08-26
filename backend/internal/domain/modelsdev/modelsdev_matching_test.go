package modelsdev

import "testing"

func TestExactMatchesUniquenessRules(t *testing.T) {
	catalog := loadFixtureCatalog(t)
	openaiMatches := ExactMatches(catalog, "openai", "gpt-test")
	if len(openaiMatches) != 1 || openaiMatches[0].ProviderID != "openai" {
		t.Fatalf("unique openai match expected, got %+v", openaiMatches)
	}
	if shared := ExactMatches(catalog, "anthropic", "shared-model"); len(shared) != 1 {
		t.Fatalf("anthropic scope must ignore azure's shared-model, got %+v", shared)
	}
	if matches := ExactMatches(catalog, "unknown-family", "gpt-test"); len(matches) != 0 {
		t.Fatalf("unmapped family must have no candidates, got %+v", matches)
	}
	if matches := ExactMatches(catalog, "openai", ""); len(matches) != 0 {
		t.Fatalf("blank id must match nothing, got %+v", matches)
	}
	if matches := ExactMatches(nil, "openai", "gpt-test"); matches != nil {
		t.Fatal("nil catalog yields no candidates")
	}
}

func TestSearchCandidatesBoundedAndScoped(t *testing.T) {
	catalog := loadFixtureCatalog(t)
	familyItems, familyTotal := SearchCandidates(catalog, "gemini", "", "family", 20, 0)
	if familyTotal != 1 || len(familyItems) != 1 || familyItems[0].ModelID != "gemini-test" {
		t.Fatalf("gemini family search = %+v total %d", familyItems, familyTotal)
	}
	allItems, allTotal := SearchCandidates(catalog, "openai", "shared", "all", 20, 0)
	if allTotal != 2 || len(allItems) != 2 {
		t.Fatalf("all-scope shared search = %+v total %d", allItems, allTotal)
	}
	page, pageTotal := SearchCandidates(catalog, "openai", "", "all", 2, 0)
	if len(page) != 2 || pageTotal <= 2 {
		t.Fatalf("bounded page = %d items, total %d", len(page), pageTotal)
	}
	overflow, overflowTotal := SearchCandidates(catalog, "openai", "", "all", 20, 999)
	if len(overflow) != 0 || overflowTotal != pageTotal {
		t.Fatalf("offset beyond end must be empty with stable total: %d vs %d", overflowTotal, pageTotal)
	}
	huge, _ := SearchCandidates(catalog, "openai", "", "all", 5000, 0)
	if len(huge) > 100 {
		t.Fatalf("candidate limit must clamp to 100, got %d", len(huge))
	}
}

func TestAutoMatchProviderIDsStable(t *testing.T) {
	if ids := AutoMatchProviderIDs("openai"); len(ids) != 1 || ids[0] != "openai" {
		t.Fatalf("openai mapping = %v", ids)
	}
	if ids := AutoMatchProviderIDs("gemini"); len(ids) != 1 || ids[0] != "google" {
		t.Fatalf("gemini mapping = %v", ids)
	}
	if ids := AutoMatchProviderIDs("custom"); len(ids) != 0 {
		t.Fatalf("unmapped families carry no providers: %v", ids)
	}
}
