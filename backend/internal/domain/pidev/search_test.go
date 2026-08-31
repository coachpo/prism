package pidev

import (
	"fmt"
	"strings"
	"testing"
)

func searchFixtureCatalog(t *testing.T) *Catalog {
	t.Helper()
	const payload = `{
  "zeta-provider": {
    "GPT-Mini": {"id": "GPT-Mini", "api": "openai-responses", "provider": "zeta-provider"},
    "gpt-mini": {"id": "gpt-mini", "api": "openai-responses", "provider": "zeta-provider"}
  },
  "alpha-provider": {
    "gpt-mini-turbo": {"id": "gpt-mini-turbo", "api": "openai-responses", "provider": "alpha-provider"},
    "prefix-gpt-mini": {"id": "prefix-gpt-mini", "api": "openai-responses", "provider": "alpha-provider"},
    "gpt-mini": {"id": "gpt-mini", "api": "openai-completions", "provider": "alpha-provider"},
    "gpt-mini-anthropic": {"id": "gpt-mini-anthropic", "api": "anthropic-messages", "provider": "alpha-provider"},
    "unrelated": {"id": "unrelated", "api": "openai-responses", "provider": "alpha-provider", "name": "GPT Mini Named"}
  }
}`
	providers, err := parseCatalog([]byte(payload))
	if err != nil {
		t.Fatalf("parse fixture catalog: %v", err)
	}
	return &Catalog{Revision: "sha256-fixture", Providers: providers}
}

func coordinateIDs(models []*Model) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.ProviderID+"/"+model.ModelID)
	}
	return out
}

func TestSearchModelIDsIsCaseInsensitiveLiteralModelIdOnly(t *testing.T) {
	catalog := searchFixtureCatalog(t)

	upper, total := catalog.SearchModelIDs("GPT-MINI", APIOpenAIResponses, 0, 0)
	if total != 4 {
		t.Fatalf("case-insensitive search total = %d, want 4 (%v)", total, coordinateIDs(upper))
	}
	if got := coordinateIDs(upper)[:2]; strings.Join(got, ",") != "zeta-provider/GPT-Mini,zeta-provider/gpt-mini" {
		t.Fatalf("exact tier must hold both case variants first, got %v", got)
	}

	if page, total := catalog.SearchModelIDs("alpha-provider", APIOpenAIResponses, 0, 0); total != 0 || len(page) != 0 {
		t.Fatalf("provider-id query must not match: total=%d page=%v", total, coordinateIDs(page))
	}
	if page, total := catalog.SearchModelIDs("Named", APIOpenAIResponses, 0, 0); total != 0 || len(page) != 0 {
		t.Fatalf("display-name query must not match: total=%d page=%v", total, coordinateIDs(page))
	}
	if page, total := catalog.SearchModelIDs("mini-gpt", APIOpenAIResponses, 0, 0); total != 0 || len(page) != 0 {
		t.Fatalf("reordered query must not fuzzy-match: total=%d page=%v", total, coordinateIDs(page))
	}
	if page, total := catalog.SearchModelIDs("gpt*mini", APIOpenAIResponses, 0, 0); total != 0 || len(page) != 0 {
		t.Fatalf("wildcard query must not match: total=%d page=%v", total, coordinateIDs(page))
	}
	for _, query := range []string{"", "   ", "\t\n"} {
		if page, total := catalog.SearchModelIDs(query, APIOpenAIResponses, 0, 0); total != 0 || len(page) != 0 {
			t.Fatalf("blank query %q must return nothing: total=%d page=%v", query, total, coordinateIDs(page))
		}
	}
	if page, total := catalog.SearchModelIDs("gpt-mini", "", 0, 0); total != 0 || len(page) != 0 {
		t.Fatalf("empty expected API must return nothing: total=%d page=%v", total, coordinateIDs(page))
	}
}

func TestSearchModelIDsNeverCrossesPiAPI(t *testing.T) {
	catalog := searchFixtureCatalog(t)

	responses, total := catalog.SearchModelIDs("gpt-mini", APIOpenAIResponses, 0, 0)
	if total != 4 {
		t.Fatalf("responses search total = %d, want 4: %v", total, coordinateIDs(responses))
	}
	for _, model := range responses {
		if model.API != APIOpenAIResponses {
			t.Fatalf("cross-API hit %s/%s api=%s", model.ProviderID, model.ModelID, model.API)
		}
	}
	completions, total := catalog.SearchModelIDs("gpt-mini", APIOpenAICompletions, 0, 0)
	if total != 1 || completions[0].ProviderID != "alpha-provider" || completions[0].ModelID != "gpt-mini" {
		t.Fatalf("completions search = %v (total %d), want only alpha-provider/gpt-mini", coordinateIDs(completions), total)
	}
	anthropic, total := catalog.SearchModelIDs("gpt-mini", APIAnthropicMessages, 0, 0)
	if total != 1 || anthropic[0].ModelID != "gpt-mini-anthropic" {
		t.Fatalf("anthropic search = %v (total %d), want only gpt-mini-anthropic", coordinateIDs(anthropic), total)
	}
	if gemini, total := catalog.SearchModelIDs("gpt-mini", APIGoogleGenerative, 0, 0); total != 0 {
		t.Fatalf("gemini search must be empty, got %v (total %d)", coordinateIDs(gemini), total)
	}
}

func TestSearchModelIDsRanksExactThenPrefixThenSubstringStably(t *testing.T) {
	catalog := searchFixtureCatalog(t)

	page, total := catalog.SearchModelIDs("gpt-mini", APIOpenAIResponses, 0, 0)
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	want := []string{
		"zeta-provider/GPT-Mini", "zeta-provider/gpt-mini",
		"alpha-provider/gpt-mini-turbo",
		"alpha-provider/prefix-gpt-mini",
	}
	got := coordinateIDs(page)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ranking = %v, want %v", got, want)
	}

}

func TestSearchModelIDsIsBoundedByDefaultTwentyAndMaxHundred(t *testing.T) {
	const providerID = "bulk"
	entries := make([]string, 0, 250)
	for index := 0; index < 150; index++ {
		entries = append(entries, fmt.Sprintf(`"mid-%03d-gpt-target": {"id": "mid-%03d-gpt-target", "api": "openai-responses", "provider": "bulk"}`, index, index))
	}
	for index := 0; index < 30; index++ {
		entries = append(entries, fmt.Sprintf(`"gpt-target-prefix-%02d": {"id": "gpt-target-prefix-%02d", "api": "openai-responses", "provider": "bulk"}`, index, index))
	}
	entries = append(entries, `"gpt-target": {"id": "gpt-target", "api": "openai-responses", "provider": "bulk"}`)
	providers, err := parseCatalog([]byte(`{"` + providerID + `":{` + strings.Join(entries, ",") + `}}`))
	if err != nil {
		t.Fatalf("parse bulk catalog: %v", err)
	}
	catalog := &Catalog{Providers: providers}

	defaultPage, total := catalog.SearchModelIDs("gpt-target", APIOpenAIResponses, 0, 0)
	if total != 181 {
		t.Fatalf("total = %d, want all 181 matches counted", total)
	}
	if len(defaultPage) != SearchDefaultLimit {
		t.Fatalf("default page size = %d, want %d", len(defaultPage), SearchDefaultLimit)
	}
	if defaultPage[0].ModelID != "gpt-target" {
		t.Fatalf("exact match must rank first even in a large revision, got %s", defaultPage[0].ModelID)
	}
	if defaultPage[1].ModelID != "gpt-target-prefix-00" {
		t.Fatalf("prefix tier must precede substring tier, got %s", defaultPage[1].ModelID)
	}

	if page, _ := catalog.SearchModelIDs("gpt-target", APIOpenAIResponses, 500, 0); len(page) != SearchMaxLimit {
		t.Fatalf("oversized limit = %d, want clamp to %d", len(page), SearchMaxLimit)
	}
	if page, _ := catalog.SearchModelIDs("gpt-target", APIOpenAIResponses, -5, 0); len(page) != SearchDefaultLimit {
		t.Fatalf("negative limit must fall back to the default page, got %d", len(page))
	}
	if page, _ := catalog.SearchModelIDs("gpt-target", APIOpenAIResponses, 5, 0); len(page) != 5 {
		t.Fatalf("explicit limit ignored, got %d", len(page))
	}
}

func TestSearchModelIDsOffsetWindowsTheRankedSet(t *testing.T) {
	catalog := searchFixtureCatalog(t)

	// The ranked order is stable, so offset windows are deterministic pages
	// over the same hit set, never a second ranking.
	first, total := catalog.SearchModelIDs("gpt-mini", APIOpenAIResponses, 2, 0)
	second, secondTotal := catalog.SearchModelIDs("gpt-mini", APIOpenAIResponses, 2, 2)
	beyond, beyondTotal := catalog.SearchModelIDs("gpt-mini", APIOpenAIResponses, 2, 4)
	if total != 4 || secondTotal != 4 || beyondTotal != 4 {
		t.Fatalf("offset must not change the total: %d/%d/%d", total, secondTotal, beyondTotal)
	}
	full, _ := catalog.SearchModelIDs("gpt-mini", APIOpenAIResponses, 20, 0)
	tiled := append(append([]string{}, coordinateIDs(first)...), coordinateIDs(second)...)
	if strings.Join(tiled, ",") != strings.Join(coordinateIDs(full), ",") {
		t.Fatalf("offset pages must tile the ranked set: %v + %v vs %v", coordinateIDs(first), coordinateIDs(second), coordinateIDs(full))
	}
	if len(beyond) != 0 {
		t.Fatalf("offset at total must return an empty page, got %v", coordinateIDs(beyond))
	}
	// A negative offset is the caller's validation error; the domain layer
	// degrades it to the first page rather than panicking or wrapping.
	negative, negativeTotal := catalog.SearchModelIDs("gpt-mini", APIOpenAIResponses, 2, -1)
	if negativeTotal != 4 || len(negative) != 2 || negative[0].ModelID != full[0].ModelID {
		t.Fatalf("negative offset must degrade to the first page: %v", coordinateIDs(negative))
	}
}

func TestSearchModelIDsSameAPIHitsShareOneRankingAcrossWindows(t *testing.T) {
	const providerID = "bulk"
	entries := make([]string, 0, 60)
	for index := 0; index < 50; index++ {
		entries = append(entries, fmt.Sprintf(`"bulk-gpt-%03d": {"id": "bulk-gpt-%03d", "api": "openai-responses", "provider": "bulk"}`, index, index))
	}
	entries = append(entries, `"bulk-gpt": {"id": "bulk-gpt", "api": "openai-responses", "provider": "bulk"}`)
	providers, err := parseCatalog([]byte(`{"` + providerID + `":{` + strings.Join(entries, ",") + `}}`))
	if err != nil {
		t.Fatalf("parse bulk catalog: %v", err)
	}
	catalog := &Catalog{Providers: providers}

	pageOne, total := catalog.SearchModelIDs("bulk-gpt", APIOpenAIResponses, 20, 0)
	pageTwo, pageTwoTotal := catalog.SearchModelIDs("bulk-gpt", APIOpenAIResponses, 20, 20)
	if total != 51 || pageTwoTotal != 51 {
		t.Fatalf("total must count the whole same-API hit set: %d/%d", total, pageTwoTotal)
	}
	if len(pageOne) != 20 || len(pageTwo) != 20 {
		t.Fatalf("page sizes drifted: %d/%d", len(pageOne), len(pageTwo))
	}
	seen := map[string]bool{}
	for _, model := range append(append([]*Model{}, pageOne...), pageTwo...) {
		key := model.ProviderID + "/" + model.ModelID
		if seen[key] {
			t.Fatalf("offset pages must not repeat %s", key)
		}
		seen[key] = true
	}
	if !seen["bulk/bulk-gpt"] {
		t.Fatalf("exact match must appear on the first page: %v", coordinateIDs(pageOne))
	}
}
