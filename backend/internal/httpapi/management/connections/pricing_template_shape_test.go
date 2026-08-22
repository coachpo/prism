package connections

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

func TestPricingTemplateDecodedCardRequiresAllFiveKeys(t *testing.T) {
	decode := func(raw string) pricingTemplateCreateRequest {
		t.Helper()
		decoder := json.NewDecoder(bytes.NewBufferString(raw))
		decoder.DisallowUnknownFields()
		var request pricingTemplateCreateRequest
		if err := decoder.Decode(&request); err != nil {
			t.Fatalf("decode pricing template: %v", err)
		}
		return request
	}
	missing := decode(`{"name":"missing","template_kind":"standard","card":{"input_price":"1","output_price":"2","cached_input_price":null,"cache_creation_price":null}}`)
	_, err := normalizePricingTemplateShape(missing)
	var domainErr *DomainError
	if !errors.As(err, &domainErr) || domainErr.StatusCode != 422 || domainErr.Fields["path"] != "card.reasoning_price" {
		t.Fatalf("expected field-level missing-key error, got %#v", err)
	}
	explicitNull := decode(`{"name":"explicit-null","template_kind":"standard","card":{"input_price":"1","output_price":"2","cached_input_price":null,"cache_creation_price":null,"reasoning_price":null}}`)
	if _, err := normalizePricingTemplateShape(explicitNull); err != nil {
		t.Fatalf("explicit null specialty fields must remain valid: %v", err)
	}
}

func TestPricingTemplateWindowsDigestMatchesCanonicalSQLBytes(t *testing.T) {
	windows := []terminaltarget.Window{
		{WeekdayMask: 31, StartMinute: 840, EndMinute: 1080},
		{WeekdayMask: 31, StartMinute: 540, EndMinute: 720},
		{WeekdayMask: 31, StartMinute: 540, EndMinute: 720},
	}
	canonical := "31,540,720\n31,840,1080"
	want := sha256.Sum256([]byte(canonical))
	if got := pricingTemplateWindowsDigest(windows); got != hex.EncodeToString(want[:]) {
		t.Fatalf("digest = %s, want %s for canonical bytes %q", got, hex.EncodeToString(want[:]), canonical)
	}
	if !strings.Contains(canonical, "\n") {
		t.Fatal("digest vector must include LF-separated rows")
	}
}

func testCardInput(input, output, cached, creation, reasoning string) *pricingTemplateCardInput {
	return &pricingTemplateCardInput{InputPrice: &input, OutputPrice: &output, CachedInputPrice: &cached, CacheCreationPrice: &creation, ReasoningPrice: &reasoning}
}

func TestPricingTemplateShapeVariantsAreMutuallyExclusive(t *testing.T) {
	standard, err := normalizePricingTemplateShape(pricingTemplateCreateRequest{TemplateKind: "standard", Card: testCardInput("1", "2", "0", "0", "0")})
	if err != nil || string(standard.Kind) != "standard" || len(standard.Cards) != 1 {
		t.Fatalf("standard shape = %+v, err=%v", standard, err)
	}
	threshold := 100
	tiered, err := normalizePricingTemplateShape(pricingTemplateCreateRequest{TemplateKind: "tiered", BaseCard: testCardInput("1", "2", "0", "0", "0"), Tier: &pricingTemplateTierInput{InputTokensAbove: &threshold, Card: testCardInput("3", "4", "0", "0", "0")}})
	if err != nil || string(tiered.Kind) != "tiered" || len(tiered.Cards) != 2 {
		t.Fatalf("tiered shape = %+v, err=%v", tiered, err)
	}
	peak, err := normalizePricingTemplateShape(pricingTemplateCreateRequest{TemplateKind: "peak_valley", PeakCard: testCardInput("10", "20", "0", "0", "0"), OffpeakCard: testCardInput("1", "2", "0", "0", "0"), Schedule: &pricingTemplateScheduleInput{Timezone: "Asia/Shanghai", Windows: []pricingTemplateWindowInput{{WeekdayMask: 127, StartMinute: 0, EndMinute: 1440}}}})
	if err != nil || string(peak.Kind) != "peak_valley" || len(peak.Cards) != 2 || len(peak.Windows) != 1 {
		t.Fatalf("peak shape = %+v, err=%v", peak, err)
	}
	if _, err := normalizePricingTemplateShape(pricingTemplateCreateRequest{TemplateKind: "standard", Card: testCardInput("1", "2", "0", "0", "0"), PeakCard: testCardInput("3", "4", "0", "0", "0")}); err == nil {
		t.Fatal("standard shape must reject peak-specific fields")
	}
	legacyTierPrice := "9"
	if _, err := normalizePricingTemplateShape(pricingTemplateCreateRequest{TemplateKind: "tiered", BaseCard: testCardInput("1", "2", "0", "0", "0"), Tier: &pricingTemplateTierInput{InputTokensAbove: &threshold, InputPrice: &legacyTierPrice, Card: testCardInput("3", "4", "0", "0", "0")}}); err == nil {
		t.Fatal("tier shape must reject legacy flat tier prices even when card is present")
	}
	if _, err := normalizePricingTemplateShape(pricingTemplateCreateRequest{TemplateKind: "peak_valley", PeakCard: testCardInput("1", "2", "0", "0", "0"), OffpeakCard: &pricingTemplateCardInput{InputPrice: stringPtr("1"), OutputPrice: stringPtr("2")}, Schedule: &pricingTemplateScheduleInput{Timezone: "UTC", Windows: []pricingTemplateWindowInput{{WeekdayMask: 1, StartMinute: 0, EndMinute: 10}}}}); err == nil {
		t.Fatal("peak cards must enforce specialty parity")
	}
}

func TestPricingTemplateWindowValidationAllowsFullCoverageAndRejectsZeroWindows(t *testing.T) {
	fullWeek := []pricingTemplateWindowInput{{WeekdayMask: 127, StartMinute: 0, EndMinute: 1440}}
	if _, err := normalizePricingTemplateWindows(&pricingTemplateScheduleInput{Timezone: "UTC", Windows: fullWeek}); err != nil {
		t.Fatalf("full-day all-week peak window should be valid: %v", err)
	}
	if _, err := normalizePricingTemplateWindows(&pricingTemplateScheduleInput{Timezone: "UTC", Windows: nil}); err == nil {
		t.Fatal("zero peak windows must be rejected")
	}
}

func TestPricingTemplateShapeChangeDetectionIncludesPeakCardWindowAndTimezone(t *testing.T) {
	base, err := normalizePricingTemplateShape(pricingTemplateCreateRequest{
		TemplateKind: "peak_valley",
		PeakCard:     testCardInput("10", "20", "0", "0", "0"),
		OffpeakCard:  testCardInput("1", "2", "0", "0", "0"),
		Schedule:     &pricingTemplateScheduleInput{Timezone: "UTC", Windows: []pricingTemplateWindowInput{{WeekdayMask: 1, StartMinute: 600, EndMinute: 720}}},
	})
	if err != nil {
		t.Fatalf("normalize base peak-valley shape: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(pricingTemplateShape) pricingTemplateShape
	}{
		{name: "peak card", mutate: func(shape pricingTemplateShape) pricingTemplateShape {
			card := shape.Cards["peak"]
			card.InputPrice = "11"
			shape.Cards["peak"] = card
			return shape
		}},
		{name: "window", mutate: func(shape pricingTemplateShape) pricingTemplateShape {
			shape.Windows[0].EndMinute = 721
			shape.Digest = pricingTemplateWindowsDigest(shape.Windows)
			return shape
		}},
		{name: "timezone", mutate: func(shape pricingTemplateShape) pricingTemplateShape {
			timezone := "Europe/Helsinki"
			shape.Timezone = &timezone
			return shape
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			candidate.Cards = make(map[string]pricingTemplateCard, len(base.Cards))
			for role, card := range base.Cards {
				candidate.Cards[role] = card
			}
			candidate.Windows = append([]terminaltarget.Window(nil), base.Windows...)
			if base.Timezone != nil {
				timezone := *base.Timezone
				candidate.Timezone = &timezone
			}
			if pricingTemplateShapesEqual(base, tc.mutate(candidate)) {
				t.Fatalf("%s-only change must create a new pricing revision", tc.name)
			}
		})
	}
}
