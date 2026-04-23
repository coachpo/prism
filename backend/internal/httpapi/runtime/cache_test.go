package runtime

import (
	"reflect"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

func TestSharedCachePlanningSnapshotReturnsImmutableClones(t *testing.T) {
	t.Parallel()

	cache := NewSharedCache(2 * time.Second)
	loaderCalls := 0
	strategyID := 7
	vendorName := "Vendor A"
	cachedInputPrice := "1"
	loader := func() (*planningSnapshot, error) {
		loaderCalls++
		return &planningSnapshot{
			ModelsByID: map[string]runtimeModelRecord{
				"gpt-4o": {
					ID:                    1,
					ModelID:               "gpt-4o",
					VendorName:            &vendorName,
					LoadbalanceStrategyID: &strategyID,
				},
			},
			ProxyTargetsBySourceID: map[int][]string{1: []string{"target-a"}},
			NativeTargetsByModelID: map[string]nativePlanningSnapshot{
				"gpt-4o": {
					Model: runtimeModelRecord{ID: 1, ModelID: "gpt-4o", LoadbalanceStrategyID: &strategyID},
					Strategy: &loadbalance.RuntimeStrategy{
						ID:              strategyID,
						Name:            "Adaptive",
						AutoRecoveryRaw: []byte(`{"mode":"legacy"}`),
					},
					Connections: []runtimeConnection{{
						ID:            11,
						CustomHeaders: map[string]any{"x-note": "original"},
						PricingTemplateSnapshot: &runtimePricingTemplateSnapshot{
							ID:               3,
							CachedInputPrice: &cachedInputPrice,
						},
						Endpoint: runtimeEndpoint{Name: &vendorName, APIKey: "secret"},
					}},
				},
			},
			BlocklistRules: []headerBlocklistRule{{MatchType: "exact", Pattern: "x-secret"}},
			ReportCurrency: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		}, nil
	}

	first, err := cache.loadPlanningSnapshot(time.Unix(20, 0), 42, loader)
	if err != nil {
		t.Fatalf("first planning snapshot load returned error: %v", err)
	}
	first.ProxyTargetsBySourceID[1][0] = "changed-target"
	first.BlocklistRules[0].Pattern = "changed-rule"
	model := first.ModelsByID["gpt-4o"]
	model.ModelID = "changed-model"
	first.ModelsByID["gpt-4o"] = model
	native := first.NativeTargetsByModelID["gpt-4o"]
	native.Strategy.AutoRecoveryRaw[0] = '['
	native.Connections[0].CustomHeaders["x-note"] = "changed"
	native.Connections[0].Endpoint.APIKey = "changed-secret"
	first.NativeTargetsByModelID["gpt-4o"] = native

	second, err := cache.loadPlanningSnapshot(time.Unix(21, 0), 42, loader)
	if err != nil {
		t.Fatalf("second planning snapshot load returned error: %v", err)
	}
	if loaderCalls != 1 {
		t.Fatalf("expected snapshot to come from cache, loader called %d times", loaderCalls)
	}
	if second.ProxyTargetsBySourceID[1][0] != "target-a" {
		t.Fatalf("expected proxy targets to remain immutable, got %v", second.ProxyTargetsBySourceID[1])
	}
	if second.BlocklistRules[0].Pattern != "x-secret" {
		t.Fatalf("expected blocklist rules to remain immutable, got %+v", second.BlocklistRules[0])
	}
	if second.ModelsByID["gpt-4o"].ModelID != "gpt-4o" {
		t.Fatalf("expected runtime model record to remain immutable, got %+v", second.ModelsByID["gpt-4o"])
	}
	gotHeaders := second.NativeTargetsByModelID["gpt-4o"].Connections[0].CustomHeaders
	if !reflect.DeepEqual(gotHeaders, map[string]any{"x-note": "original"}) {
		t.Fatalf("expected custom headers to remain immutable, got %v", gotHeaders)
	}
	if second.NativeTargetsByModelID["gpt-4o"].Connections[0].Endpoint.APIKey != "secret" {
		t.Fatalf("expected endpoint snapshot to remain immutable, got %+v", second.NativeTargetsByModelID["gpt-4o"].Connections[0].Endpoint)
	}
	if string(second.NativeTargetsByModelID["gpt-4o"].Strategy.AutoRecoveryRaw) != `{"mode":"legacy"}` {
		t.Fatalf("expected runtime strategy bytes to remain immutable, got %s", second.NativeTargetsByModelID["gpt-4o"].Strategy.AutoRecoveryRaw)
	}
}
