package runtime

import (
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/profiledomain"
)

const sharedCacheTestConcurrency = 8

type sharedCacheActiveProfileResult struct {
	value profiledomain.Profile
	err   error
}

type sharedCachePlanningSnapshotResult struct {
	value *planningSnapshot
	err   error
}

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

func TestSharedCacheCoalescesPlanningReload(t *testing.T) {
	t.Parallel()

	cache := NewSharedCache(2 * time.Second)
	var loaderCalls atomic.Int32
	start := make(chan struct{})
	entered := make(chan struct{}, sharedCacheTestConcurrency)
	loaderStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	results := make(chan sharedCachePlanningSnapshotResult, sharedCacheTestConcurrency)

	loader := func() (*planningSnapshot, error) {
		loaderCalls.Add(1)
		signalOnce(loaderStarted)
		<-release
		return newSharedCachePlanningSnapshot("coalesced-planning"), nil
	}

	for range sharedCacheTestConcurrency {
		go func() {
			<-start
			entered <- struct{}{}
			value, err := cache.loadPlanningSnapshot(time.Unix(10, 0), 42, loader)
			results <- sharedCachePlanningSnapshotResult{value: value, err: err}
		}()
	}

	close(start)
	waitForSignals(t, entered, sharedCacheTestConcurrency, "planning callers to start")
	waitForSignal(t, loaderStarted, "planning loader to start")
	close(release)

	for range sharedCacheTestConcurrency {
		result := <-results
		if result.err != nil {
			t.Fatalf("expected planning reload to succeed, got error: %v", result.err)
		}
		if result.value == nil || result.value.ReportCurrency.Code != "coalesced-planning" {
			t.Fatalf("expected shared planning snapshot, got %+v", result.value)
		}
	}
	if got := loaderCalls.Load(); got != 1 {
		t.Fatalf("expected exactly one planning loader call, got %d", got)
	}
}

func TestSharedCacheCoalescesActiveProfileReload(t *testing.T) {
	t.Parallel()

	cache := NewSharedCache(2 * time.Second)
	var loaderCalls atomic.Int32
	start := make(chan struct{})
	entered := make(chan struct{}, sharedCacheTestConcurrency)
	loaderStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	results := make(chan sharedCacheActiveProfileResult, sharedCacheTestConcurrency)

	loader := func() (profiledomain.Profile, error) {
		loaderCalls.Add(1)
		signalOnce(loaderStarted)
		<-release
		return newSharedCacheProfile(11, "coalesced-active"), nil
	}

	for range sharedCacheTestConcurrency {
		go func() {
			<-start
			entered <- struct{}{}
			value, err := cache.loadActiveProfile(time.Unix(10, 0), loader)
			results <- sharedCacheActiveProfileResult{value: value, err: err}
		}()
	}

	close(start)
	waitForSignals(t, entered, sharedCacheTestConcurrency, "active profile callers to start")
	waitForSignal(t, loaderStarted, "active profile loader to start")
	close(release)

	for range sharedCacheTestConcurrency {
		result := <-results
		if result.err != nil {
			t.Fatalf("expected active profile reload to succeed, got error: %v", result.err)
		}
		if result.value.ID != 11 || result.value.Name != "coalesced-active" {
			t.Fatalf("expected shared active profile, got %+v", result.value)
		}
	}
	if got := loaderCalls.Load(); got != 1 {
		t.Fatalf("expected exactly one active profile loader call, got %d", got)
	}
}

func TestSharedCacheFailureIsSharedNotCached(t *testing.T) {
	t.Parallel()

	t.Run("active-profile", func(t *testing.T) {
		cache := NewSharedCache(2 * time.Second)
		var loaderCalls atomic.Int32
		wantErr := errors.New("active profile reload failed")
		loaderStarted := make(chan struct{}, 1)
		release := make(chan struct{})
		results := make(chan sharedCacheActiveProfileResult, 2)
		start := make(chan struct{})
		entered := make(chan struct{}, 2)

		loader := func() (profiledomain.Profile, error) {
			call := loaderCalls.Add(1)
			signalOnce(loaderStarted)
			<-release
			if call == 1 {
				return profiledomain.Profile{}, wantErr
			}
			return newSharedCacheProfile(22, "active-after-failure"), nil
		}

		for range 2 {
			go func() {
				<-start
				entered <- struct{}{}
				value, err := cache.loadActiveProfile(time.Unix(30, 0), loader)
				results <- sharedCacheActiveProfileResult{value: value, err: err}
			}()
		}

		close(start)
		waitForSignals(t, entered, 2, "active profile failure callers to start")
		waitForSignal(t, loaderStarted, "active profile failure loader to start")
		close(release)

		for range 2 {
			result := <-results
			if !errors.Is(result.err, wantErr) {
				t.Fatalf("expected shared active profile error %v, got %v", wantErr, result.err)
			}
		}
		if got := loaderCalls.Load(); got != 1 {
			t.Fatalf("expected exactly one failing active profile loader call, got %d", got)
		}

		value, err := cache.loadActiveProfile(time.Unix(31, 0), loader)
		if err != nil {
			t.Fatalf("expected active profile reload after failure to succeed, got %v", err)
		}
		if value.ID != 22 {
			t.Fatalf("expected active profile reload after failure, got %+v", value)
		}
		cachedValue, err := cache.loadActiveProfile(time.Unix(31, 1), loader)
		if err != nil {
			t.Fatalf("expected cached active profile after recovery, got %v", err)
		}
		if cachedValue.ID != 22 {
			t.Fatalf("expected recovered active profile to be cached, got %+v", cachedValue)
		}
		if got := loaderCalls.Load(); got != 2 {
			t.Fatalf("expected failure not to be cached as success, loader called %d times", got)
		}
	})

	t.Run("planning-snapshot", func(t *testing.T) {
		cache := NewSharedCache(2 * time.Second)
		var loaderCalls atomic.Int32
		wantErr := errors.New("planning reload failed")
		loaderStarted := make(chan struct{}, 1)
		release := make(chan struct{})
		results := make(chan sharedCachePlanningSnapshotResult, 2)
		start := make(chan struct{})
		entered := make(chan struct{}, 2)

		loader := func() (*planningSnapshot, error) {
			call := loaderCalls.Add(1)
			signalOnce(loaderStarted)
			<-release
			if call == 1 {
				return nil, wantErr
			}
			return newSharedCachePlanningSnapshot("planning-after-failure"), nil
		}

		for range 2 {
			go func() {
				<-start
				entered <- struct{}{}
				value, err := cache.loadPlanningSnapshot(time.Unix(40, 0), 77, loader)
				results <- sharedCachePlanningSnapshotResult{value: value, err: err}
			}()
		}

		close(start)
		waitForSignals(t, entered, 2, "planning failure callers to start")
		waitForSignal(t, loaderStarted, "planning failure loader to start")
		close(release)

		for range 2 {
			result := <-results
			if !errors.Is(result.err, wantErr) {
				t.Fatalf("expected shared planning error %v, got %v", wantErr, result.err)
			}
		}
		if got := loaderCalls.Load(); got != 1 {
			t.Fatalf("expected exactly one failing planning loader call, got %d", got)
		}

		value, err := cache.loadPlanningSnapshot(time.Unix(41, 0), 77, loader)
		if err != nil {
			t.Fatalf("expected planning reload after failure to succeed, got %v", err)
		}
		if value == nil || value.ReportCurrency.Code != "planning-after-failure" {
			t.Fatalf("expected planning reload after failure, got %+v", value)
		}
		cachedValue, err := cache.loadPlanningSnapshot(time.Unix(41, 1), 77, loader)
		if err != nil {
			t.Fatalf("expected cached planning snapshot after recovery, got %v", err)
		}
		if cachedValue == nil || cachedValue.ReportCurrency.Code != "planning-after-failure" {
			t.Fatalf("expected recovered planning snapshot to be cached, got %+v", cachedValue)
		}
		if got := loaderCalls.Load(); got != 2 {
			t.Fatalf("expected failure not to be cached as success, loader called %d times", got)
		}
	})
}

func TestSharedCacheInvalidationAfterCoalescedLoad(t *testing.T) {
	t.Parallel()

	t.Run("active-profile", func(t *testing.T) {
		cache := NewSharedCache(2 * time.Second)
		var loaderCalls atomic.Int32
		firstStarted := make(chan struct{}, 1)
		secondStarted := make(chan struct{}, 1)
		firstRelease := make(chan struct{})
		secondRelease := make(chan struct{})
		firstResult := make(chan sharedCacheActiveProfileResult, 1)
		secondResult := make(chan sharedCacheActiveProfileResult, 1)

		loader := func() (profiledomain.Profile, error) {
			switch loaderCalls.Add(1) {
			case 1:
				signalOnce(firstStarted)
				<-firstRelease
				return newSharedCacheProfile(31, "stale-active"), nil
			case 2:
				signalOnce(secondStarted)
				<-secondRelease
				return newSharedCacheProfile(32, "fresh-active"), nil
			default:
				return profiledomain.Profile{}, fmt.Errorf("unexpected active profile loader call")
			}
		}

		go func() {
			value, err := cache.loadActiveProfile(time.Unix(50, 0), loader)
			firstResult <- sharedCacheActiveProfileResult{value: value, err: err}
		}()

		waitForSignal(t, firstStarted, "first active profile loader to start")
		cache.InvalidateActiveProfile()

		go func() {
			value, err := cache.loadActiveProfile(time.Unix(50, 0), loader)
			secondResult <- sharedCacheActiveProfileResult{value: value, err: err}
		}()

		waitForSignal(t, secondStarted, "second active profile loader to start")
		close(firstRelease)
		first := <-firstResult
		if first.err != nil {
			t.Fatalf("expected first active profile load to succeed, got %v", first.err)
		}
		if first.value.ID != 31 {
			t.Fatalf("expected first active profile load to return stale result, got %+v", first.value)
		}

		close(secondRelease)
		second := <-secondResult
		if second.err != nil {
			t.Fatalf("expected second active profile load to succeed, got %v", second.err)
		}
		if second.value.ID != 32 {
			t.Fatalf("expected invalidation to force fresh active profile result, got %+v", second.value)
		}

		cachedValue, err := cache.loadActiveProfile(time.Unix(50, 1), func() (profiledomain.Profile, error) {
			return profiledomain.Profile{}, fmt.Errorf("stale active profile wrote back into cache")
		})
		if err != nil {
			t.Fatalf("expected fresh active profile to remain cached, got %v", err)
		}
		if cachedValue.ID != 32 {
			t.Fatalf("expected cached active profile to use fresh value after invalidation, got %+v", cachedValue)
		}
		if got := loaderCalls.Load(); got != 2 {
			t.Fatalf("expected invalidation to trigger exactly one replacement active profile load, got %d calls", got)
		}
	})

	t.Run("planning-snapshot", func(t *testing.T) {
		cache := NewSharedCache(2 * time.Second)
		var loaderCalls atomic.Int32
		firstStarted := make(chan struct{}, 1)
		secondStarted := make(chan struct{}, 1)
		firstRelease := make(chan struct{})
		secondRelease := make(chan struct{})
		firstResult := make(chan sharedCachePlanningSnapshotResult, 1)
		secondResult := make(chan sharedCachePlanningSnapshotResult, 1)

		loader := func() (*planningSnapshot, error) {
			switch loaderCalls.Add(1) {
			case 1:
				signalOnce(firstStarted)
				<-firstRelease
				return newSharedCachePlanningSnapshot("stale-planning"), nil
			case 2:
				signalOnce(secondStarted)
				<-secondRelease
				return newSharedCachePlanningSnapshot("fresh-planning"), nil
			default:
				return nil, fmt.Errorf("unexpected planning loader call")
			}
		}

		go func() {
			value, err := cache.loadPlanningSnapshot(time.Unix(60, 0), 88, loader)
			firstResult <- sharedCachePlanningSnapshotResult{value: value, err: err}
		}()

		waitForSignal(t, firstStarted, "first planning loader to start")
		cache.InvalidatePlanningProfile(88)

		go func() {
			value, err := cache.loadPlanningSnapshot(time.Unix(60, 0), 88, loader)
			secondResult <- sharedCachePlanningSnapshotResult{value: value, err: err}
		}()

		waitForSignal(t, secondStarted, "second planning loader to start")
		close(firstRelease)
		first := <-firstResult
		if first.err != nil {
			t.Fatalf("expected first planning load to succeed, got %v", first.err)
		}
		if first.value == nil || first.value.ReportCurrency.Code != "stale-planning" {
			t.Fatalf("expected first planning load to return stale result, got %+v", first.value)
		}

		close(secondRelease)
		second := <-secondResult
		if second.err != nil {
			t.Fatalf("expected second planning load to succeed, got %v", second.err)
		}
		if second.value == nil || second.value.ReportCurrency.Code != "fresh-planning" {
			t.Fatalf("expected invalidation to force fresh planning result, got %+v", second.value)
		}

		cachedValue, err := cache.loadPlanningSnapshot(time.Unix(60, 1), 88, func() (*planningSnapshot, error) {
			return nil, fmt.Errorf("stale planning snapshot wrote back into cache")
		})
		if err != nil {
			t.Fatalf("expected fresh planning snapshot to remain cached, got %v", err)
		}
		if cachedValue == nil || cachedValue.ReportCurrency.Code != "fresh-planning" {
			t.Fatalf("expected cached planning snapshot to use fresh value after invalidation, got %+v", cachedValue)
		}
		if got := loaderCalls.Load(); got != 2 {
			t.Fatalf("expected invalidation to trigger exactly one replacement planning load, got %d calls", got)
		}
	})
}

func newSharedCacheProfile(id int, name string) profiledomain.Profile {
	description := name + " description"
	return profiledomain.Profile{
		ID:          id,
		Name:        name,
		Description: &description,
		IsActive:    true,
		Version:     id,
	}
}

func newSharedCachePlanningSnapshot(code string) *planningSnapshot {
	return &planningSnapshot{
		ModelsByID: map[string]runtimeModelRecord{
			code: {ID: 1, ModelID: code},
		},
		ReportCurrency: runtimeReportCurrencySnapshot{Code: code},
	}
}

func signalOnce(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForSignals(t *testing.T, ch <-chan struct{}, count int, description string) {
	t.Helper()
	for i := 0; i < count; i++ {
		waitForSignal(t, ch, fmt.Sprintf("%s (%d/%d)", description, i+1, count))
	}
}
