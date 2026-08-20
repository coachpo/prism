package connections

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

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
