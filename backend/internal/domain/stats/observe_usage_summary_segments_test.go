package stats

import "testing"

func TestDecodeObserveCostSegmentsPreservesCardRoleBreakdown(t *testing.T) {
	segments, err := decodeObserveCostSegments(1, []byte(`[{
		"segment_key":"e.1",
		"observed_symbols":[],
		"unpriced_reason_counts":{},
		"pricing_card_role_breakdown":[
			{"card_role":"peak","request_count":2,"priced_request_count":2,"known_cost_micros":"40"},
			{"card_role":"offpeak","request_count":1,"priced_request_count":1,"known_cost_micros":"10"}
		]
	}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 || len(segments[0].PricingCardRoleBreakdown) != 2 || segments[0].PricingCardRoleBreakdown[0].CardRole != "peak" {
		t.Fatalf("card-role breakdown was dropped: %#v", segments)
	}

	empty, err := decodeObserveCostSegments(1, []byte(`[{"segment_key":"e.2","observed_symbols":[]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if empty[0].PricingCardRoleBreakdown == nil {
		t.Fatal("missing card-role breakdown must encode as [] rather than null")
	}
}
