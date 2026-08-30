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

	upper, total := catalog.SearchModelIDs("GPT-MINI", APIOpenAIResponses, 0)
	if total != 4 {
		t.Fatalf("case-insensitive search total = %d, want 4 (%v)", total, coordinateIDs(upper))
	}
	if got := coordinateIDs(upper)[:2]; strings.Join(got, ",") != "zeta-provider/GPT-Mini,zeta-provider/gpt-mini" {
		t.Fatalf("exact tier must hold both case variants first, got %v", got)
	}

	if page, total := catalog.SearchModelIDs("alpha-provider", APIOpenAIResponses, 0); total != 0 || len(page) != 0 {
		t.Fatalf("provider-id query must not match: total=%d page=%v", total, coordinateIDs(page))
	}
	if page, total := catalog.SearchModelIDs("Named", APIOpenAIResponses, 0); total != 0 || len(page) != 0 {
		t.Fatalf("display-name query must not match: total=%d page=%v", total, coordinateIDs(page))
	}
	if page, total := catalog.SearchModelIDs("mini-gpt", APIOpenAIResponses, 0); total != 0 || len(page) != 0 {
		t.Fatalf("reordered query must not fuzzy-match: total=%d page=%v", total, coordinateIDs(page))
	}
	if page, total := catalog.SearchModelIDs("gpt*mini", APIOpenAIResponses, 0); total != 0 || len(page) != 0 {
		t.Fatalf("wildcard query must not match: total=%d page=%v", total, coordinateIDs(page))
	}
	for _, query := range []string{"", "   ", "\t\n"} {
		if page, total := catalog.SearchModelIDs(query, APIOpenAIResponses, 0); total != 0 || len(page) != 0 {
			t.Fatalf("blank query %q must return nothing: total=%d page=%v", query, total, coordinateIDs(page))
		}
	}
	if page, total := catalog.SearchModelIDs("gpt-mini", "", 0); total != 0 || len(page) != 0 {
		t.Fatalf("empty expected API must return nothing: total=%d page=%v", total, coordinateIDs(page))
	}
}

func TestSearchModelIDsNeverCrossesPiAPI(t *testing.T) {
	catalog := searchFixtureCatalog(t)

	responses, total := catalog.SearchModelIDs("gpt-mini", APIOpenAIResponses, 0)
	if total != 4 {
		t.Fatalf("responses search total = %d, want 4: %v", total, coordinateIDs(responses))
	}
	for _, model := range responses {
		if model.API != APIOpenAIResponses {
			t.Fatalf("cross-API hit %s/%s api=%s", model.ProviderID, model.ModelID, model.API)
		}
	}
	completions, total := catalog.SearchModelIDs("gpt-mini", APIOpenAICompletions, 0)
	if total != 1 || completions[0].ProviderID != "alpha-provider" || completions[0].ModelID != "gpt-mini" {
		t.Fatalf("completions search = %v (total %d), want only alpha-provider/gpt-mini", coordinateIDs(completions), total)
	}
	anthropic, total := catalog.SearchModelIDs("gpt-mini", APIAnthropicMessages, 0)
	if total != 1 || anthropic[0].ModelID != "gpt-mini-anthropic" {
		t.Fatalf("anthropic search = %v (total %d), want only gpt-mini-anthropic", coordinateIDs(anthropic), total)
	}
	if gemini, total := catalog.SearchModelIDs("gpt-mini", APIGoogleGenerative, 0); total != 0 {
		t.Fatalf("gemini search must be empty, got %v (total %d)", coordinateIDs(gemini), total)
	}
}

func TestSearchModelIDsRanksExactThenPrefixThenSubstringStably(t *testing.T) {
	catalog := searchFixtureCatalog(t)

	page, total := catalog.SearchModelIDs("gpt-mini", APIOpenAIResponses, 0)
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

	defaultPage, total := catalog.SearchModelIDs("gpt-target", APIOpenAIResponses, 0)
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

	if page, _ := catalog.SearchModelIDs("gpt-target", APIOpenAIResponses, 500); len(page) != SearchMaxLimit {
		t.Fatalf("oversized limit = %d, want clamp to %d", len(page), SearchMaxLimit)
	}
	if page, _ := catalog.SearchModelIDs("gpt-target", APIOpenAIResponses, -5); len(page) != SearchDefaultLimit {
		t.Fatalf("negative limit must fall back to the default page, got %d", len(page))
	}
	if page, _ := catalog.SearchModelIDs("gpt-target", APIOpenAIResponses, 5); len(page) != 5 {
		t.Fatalf("explicit limit ignored, got %d", len(page))
	}
}
