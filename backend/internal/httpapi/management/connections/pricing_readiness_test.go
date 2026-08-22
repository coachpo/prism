package connections

import (
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

func TestPricingListPeakValleyReportsOnlyMissingSpecialtyComponents(t *testing.T) {
	item := pricingTemplateListItem{CurrentRevision: pricingRevisionDTO{
		TemplateKind: string(pricingkind.PeakValley),
		PeakCard:     &pricingTemplateCard{InputPrice: "1", OutputPrice: "2"},
		OffpeakCard:  &pricingTemplateCard{InputPrice: "3", OutputPrice: "4"},
	}}
	setPricingListConfigurationStatus(&item)
	if item.ConfigurationStatus != "incomplete" {
		t.Fatalf("expected incomplete specialty configuration, got %q", item.ConfigurationStatus)
	}
	if len(item.MissingSpecialtyComponents) != 3 || item.MissingSpecialtyComponents[0] != "cached_input_price" || item.MissingSpecialtyComponents[1] != "cache_creation_price" || item.MissingSpecialtyComponents[2] != "reasoning_price" {
		t.Fatalf("unexpected missing specialty components: %#v", item.MissingSpecialtyComponents)
	}
}

func TestCostReadyAllowsUnconfiguredSpecialtyComponents(t *testing.T) {
	epoch := 4
	kind := string(pricingkind.PeakValley)
	timezone := "UTC"
	digest := "digest"
	if !isCostReadyPricingRow(&epoch, &epoch, &kind, true, &timezone, &digest, true) {
		t.Fatal("base input/output readiness must not require specialty prices")
	}
	unknown := "future_kind"
	if isCostReadyPricingRow(&epoch, &epoch, &unknown, true, nil, nil, false) {
		t.Fatal("unknown template kinds must not be reported cost-ready")
	}
}
