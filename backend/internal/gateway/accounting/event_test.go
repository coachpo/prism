package accounting

import (
	"encoding/json"
	"testing"
	"time"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

func TestEventNormalizeDoesNotMutateCallerPointers(t *testing.T) {
	kind := " peak_valley "
	state := " selected "
	role := " peak "
	threshold := 12
	basis := int64(24)
	decidedAt := time.Date(2026, 8, 22, 12, 0, 0, 0, time.FixedZone("EET", 2*60*60))
	timezone := " Europe/Helsinki "
	weekday := 6
	minute := 720
	digest := " digest "
	event := Event{Phase: EventPhaseAttempt, OperationName: " op ", AttemptNumber: 1}
	event.SetPricingEvidence(&kind, &state, &role, &threshold, &basis, &decidedAt, &timezone, &weekday, &minute, &digest)

	if event.PricingTemplateKind == &kind || event.PricingSelectorThresholdTokens == &threshold || event.PricingScheduleDecidedAt == &decidedAt {
		t.Fatal("SetPricingEvidence must clone caller-owned pointers")
	}
	normalized := event.Normalize()
	if kind != " peak_valley " || state != " selected " || role != " peak " || timezone != " Europe/Helsinki " || digest != " digest " {
		t.Fatal("Normalize mutated a caller-owned pricing pointer")
	}
	if normalized.PricingTemplateKind == nil || *normalized.PricingTemplateKind != "peak_valley" || normalized.PricingCardRole == nil || *normalized.PricingCardRole != "peak" {
		t.Fatalf("expected normalized pricing evidence, got %+v", normalized)
	}
	if normalized.PricingScheduleDecidedAt == nil || !normalized.PricingScheduleDecidedAt.Equal(decidedAt.UTC()) {
		t.Fatalf("expected UTC decision time, got %+v", normalized.PricingScheduleDecidedAt)
	}
}

func TestEventPricingEvidenceJSONShape(t *testing.T) {
	event, err := NewEvent(Event{
		Phase:         EventPhaseFinal,
		OperationName: "openai.chat_completions",
		Final:         true,
		RouteReason:   gatewaycore.RouteReasonDirectMatch,
		UsageSource:   gatewaycore.UsageSourceProvider,
		AttemptNumber: 1,
	})
	if err != nil {
		t.Fatalf("normalize event: %v", err)
	}
	role := "offpeak"
	state := "selected"
	event.SetPricingEvidence(nil, &state, &role, nil, nil, nil, nil, nil, nil, nil)
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var shape map[string]any
	if err := json.Unmarshal(payload, &shape); err != nil {
		t.Fatalf("decode event JSON: %v", err)
	}
	if shape["phase"] != string(EventPhaseFinal) || shape["operation_name"] != "openai.chat_completions" || shape["pricing_selection_state"] != "selected" || shape["pricing_card_role"] != "offpeak" {
		t.Fatalf("unexpected accounting JSON shape: %s", payload)
	}
	if _, ok := shape["pricing_template_kind"]; ok {
		t.Fatal("nil pricing template kind must remain omitted")
	}
}
