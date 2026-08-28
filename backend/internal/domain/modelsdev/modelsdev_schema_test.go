package modelsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestFetchParsesAndValidatesSchema(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"cat-1"`)
		fmt.Fprint(w, fixtureCatalog)
	}))
	catalog, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if catalog.ETag != `"cat-1"` {
		t.Fatalf("etag = %q", catalog.ETag)
	}
	model, ok := catalog.Find("openai", "gpt-test")
	if !ok {
		t.Fatal("gpt-test missing")
	}
	if model.Name != "GPT Test" || model.Description == nil || *model.Description != "fixture model" {
		t.Fatalf("unexpected metadata: %+v", model)
	}
	if model.Limit.Context == nil || *model.Limit.Context != 128000 {
		t.Fatalf("context limit = %v", model.Limit.Context)
	}
	if model.Cost.Base.Input != "2.5" || model.Cost.Base.Output != "10" {
		t.Fatalf("price canonicalization broke literals: %q/%q", model.Cost.Base.Input, model.Cost.Base.Output)
	}
	if model.Cost.Base.CachedInput == nil || *model.Cost.Base.CachedInput != "0" {
		t.Fatalf("explicit zero cache_read must stay \"0\": %v", model.Cost.Base.CachedInput)
	}
	if model.Cost.Base.CacheCreation == nil || *model.Cost.Base.CacheCreation != "1.25" {
		t.Fatalf("cache_write = %v", model.Cost.Base.CacheCreation)
	}
}

func TestFetchRejectsSchemaViolations(t *testing.T) {
	cases := map[string]string{
		"top level array":       `[{"models":{}}]`,
		"model not object":      `{"p":{"models":{"m":42}}}`,
		"id mismatch":           `{"p":{"models":{"m":{"id":"other"}}}}`,
		"bad status enum":       `{"p":{"models":{"m":{"status":"sunset"}}}}`,
		"bad modality":          `{"p":{"models":{"m":{"modalities":{"input":["smell"]}}}}}`,
		"fractional limit":      `{"p":{"models":{"m":{"limit":{"context":12.5}}}}}`,
		"negative price":        `{"p":{"models":{"m":{"cost":{"input":-1,"output":2}}}}}`,
		"input without output":  `{"p":{"models":{"m":{"cost":{"input":1}}}}}`,
		"tier missing size":     `{"p":{"models":{"m":{"cost":{"input":1,"output":2,"tiers":[{"type":"context"}]}}}}}`,
		"date format violated":  `{"p":{"models":{"m":{"release_date":"tomorrow"}}}}`,
		"empty provider id":     `{"":{"models":{}}}`,
		"context_over_200k bad": `{"p":{"models":{"m":{"cost":{"input":1,"output":2,"context_over_200k":{"input":"x","output":"y"}}}}}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, payload)
			}))
			if _, err := client.Fetch(context.Background()); err == nil {
				t.Fatal("expected schema rejection")
			}
		})
	}
}

func TestCanonicalPriceNormalizesLiterals(t *testing.T) {
	cases := map[string]string{
		"0":                     "0",
		"0.0":                   "0",
		"00":                    "0",
		"2.50":                  "2.5",
		"10":                    "10",
		"0.028":                 "0.028",
		".5":                    "0.5",
		"0.0024499999999999995": "0.0024499999999999995",
		"1e3":                   "1000",
		"1.25e-3":               "0.00125",
		"2.500E+2":              "250",
	}
	for raw, want := range cases {
		got, err := CanonicalPrice(raw)
		if err != nil || got != want {
			t.Fatalf("CanonicalPrice(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"-1", "", "1.", ".", "1e", "1e100000"} {
		if _, err := CanonicalPrice(raw); err == nil {
			t.Fatalf("CanonicalPrice(%q) must fail", raw)
		}
	}
}

func TestParseCatalogPreservesNumbersWithoutFloatRoundTrip(t *testing.T) {
	payload := `{"p":{"models":{"m":{"name":"m","cost":{"input":0.1,"output":0.30000000000000004},"limit":{"context":9999999999}}}}}`
	providers, err := parseCatalog([]byte(payload))
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	model := providers["p"].Models["m"]
	if model.Cost.Base.Output != "0.30000000000000004" {
		t.Fatalf("long decimal literal lost: %q", model.Cost.Base.Output)
	}
	if model.Limit.Context == nil || *model.Limit.Context != 9999999999 {
		t.Fatalf("large token counts must survive as int64: %v", model.Limit.Context)
	}
	raw, _ := json.Marshal(model.Cost.Base.Input)
	if string(raw) != `"0.1"` {
		t.Fatalf("canonical input should serialize back as a string, got %s", raw)
	}
}

func TestParseCatalogAcceptsOfficialNullableEffortAndLongPrice(t *testing.T) {
	payload := `{
		"sarvam":{"models":{"sarvam-105b":{
			"name":"Sarvam-105B",
			"reasoning_options":[{"type":"effort","values":[null,"low","medium","high"]}]
		}}},
		"chutes":{"models":{"Nemotron-3-Nano-Omni-30B-TEE":{
			"name":"Nemotron 3 Nano Omni 30B TEE",
			"cost":{"input":0.0245,"output":0.0978,"cache_read":0.0024499999999999995}
		}}}
	}`
	providers, err := parseCatalog([]byte(payload))
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	reasoning := providers["sarvam"].Models["sarvam-105b"].ReasoningOptions
	if len(reasoning) != 1 || len(reasoning[0].Values) != 4 || reasoning[0].Values[0] != nil {
		t.Fatalf("nullable effort values were not preserved: %+v", reasoning)
	}
	for index, want := range []string{"low", "medium", "high"} {
		value := reasoning[0].Values[index+1]
		if value == nil || *value != want {
			t.Fatalf("effort value %d = %v, want %q", index+1, value, want)
		}
	}
	model := providers["chutes"].Models["Nemotron-3-Nano-Omni-30B-TEE"]
	if model.Cost == nil || model.Cost.Base.CachedInput == nil ||
		*model.Cost.Base.CachedInput != "0.0024499999999999995" {
		t.Fatalf("long catalog price was not preserved: %+v", model.Cost)
	}
}

func TestParseReasoningOptionsAndInterleaved(t *testing.T) {
	payload := `{"p":{"models":{
		"a":{"name":"a","reasoning_options":[{"type":"effort","values":["low","medium","high"]},{"type":"toggle"}],"interleaved":{"field":"reasoning_content"}},
		"b":{"name":"b","reasoning_options":[{"type":"budget_tokens"}],"interleaved":true},
		"c":{"name":"c","interleaved":false},
		"d":{"name":"d"},
		"e":{"name":"e","reasoning_options":[]}
	}}}`
	providers, err := parseCatalog([]byte(payload))
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	a := providers["p"].Models["a"]
	if len(a.ReasoningOptions) != 2 || a.ReasoningOptions[0].Type != ReasoningOptionEffort ||
		len(a.ReasoningOptions[0].Values) != 3 || a.ReasoningOptions[1].Type != ReasoningOptionToggle {
		t.Fatalf("model a reasoning options mismatch: %+v", a.ReasoningOptions)
	}
	if a.Interleaved == nil || a.Interleaved.Kind != "field" || a.Interleaved.Field != "reasoning_content" {
		t.Fatalf("model a interleaved mismatch: %+v", a.Interleaved)
	}
	b := providers["p"].Models["b"]
	if len(b.ReasoningOptions) != 1 || b.ReasoningOptions[0].Type != ReasoningOptionBudgetTokens {
		t.Fatalf("model b reasoning options mismatch: %+v", b.ReasoningOptions)
	}
	if b.Interleaved == nil || !b.Interleaved.Bool {
		t.Fatalf("model b interleaved must be plain true: %+v", b.Interleaved)
	}
	c := providers["p"].Models["c"]
	if c.Interleaved == nil || c.Interleaved.Bool {
		t.Fatalf("model c interleaved must preserve explicit false: %+v", c.Interleaved)
	}
	if providers["p"].Models["d"].Interleaved != nil || len(providers["p"].Models["d"].ReasoningOptions) != 0 {
		t.Fatalf("absent fields must stay absent")
	}
	if len(providers["p"].Models["e"].ReasoningOptions) != 0 {
		t.Fatalf("empty reasoning_options array must normalize to none")
	}
}

func TestParseReasoningOptionsRejectsUnknownTypesAndBrokenShapes(t *testing.T) {
	for name, payload := range map[string]string{
		"unknown type":    `{"p":{"models":{"m":{"reasoning_options":[{"type":"mood"}]}}}}`,
		"missing values":  `{"p":{"models":{"m":{"reasoning_options":[{"type":"effort"}]}}}}`,
		"non-array":       `{"p":{"models":{"m":{"reasoning_options":"yes"}}}}`,
		"empty effort":    `{"p":{"models":{"m":{"reasoning_options":[{"type":"effort","values":[""]}]}}}}`,
		"broken field":    `{"p":{"models":{"m":{"interleaved":{"wrong":"x"}}}}}`,
		"numeric boolean": `{"p":{"models":{"m":{"interleaved":1}}}}`,
	} {
		if _, err := parseCatalog([]byte(payload)); err == nil {
			t.Fatalf("%s must fail schema validation", name)
		}
	}
}
