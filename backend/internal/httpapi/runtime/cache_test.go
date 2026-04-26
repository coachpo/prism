package runtime

import (
	"reflect"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestSharedCachePublishedSnapshotsCloneProfilesAndProxyKeysButSharePlanningSnapshots(t *testing.T) {
	t.Parallel()

	expiresAt := time.Unix(100, 0).UTC()
	cache := NewSharedCache(0)
	cache.published.Store(&publishedRuntimeSnapshot{
		Generation:    1,
		PublishedAt:   time.Unix(90, 0).UTC(),
		ActiveProfile: newSharedCacheProfile(11, "published-active"),
		PlanningByProfileID: map[int]*planningSnapshot{
			42: newSharedCachePlanningSnapshot("published-planning"),
		},
		Auth: publishedRuntimeAuthSnapshot{
			Settings: RuntimeAuthSettingsSnapshot{AuthEnabled: true},
			ProxyKeysByPrefix: map[string]RuntimeProxyKeyRecord{
				"pm-12345678": {KeyID: 17, KeyName: "published-key", KeyHash: "hash", ExpiresAt: &expiresAt},
			},
		},
	})

	firstProfile, err := cache.LoadPublishedActiveProfile()
	if err != nil {
		t.Fatalf("load published active profile: %v", err)
	}
	firstPlanning, err := cache.LoadPublishedPlanningSnapshot(42)
	if err != nil {
		t.Fatalf("load published planning snapshot: %v", err)
	}
	firstKey, ok, err := cache.LoadRuntimeProxyKeyRecord("pm-12345678")
	if err != nil {
		t.Fatalf("load published proxy key record: %v", err)
	}
	if !ok {
		t.Fatal("expected published proxy key record to exist")
	}

	mutatedDescription := "mutated-description"
	firstProfile.Description = &mutatedDescription
	if firstProfile.Description == nil || *firstProfile.Description != mutatedDescription {
		t.Fatalf("expected first active profile read to be locally mutable, got %+v", firstProfile)
	}
	firstPlanning.ReportCurrency.Code = "mutated-code"
	firstPlanning.ModelsByID["published-planning"] = runtimeModelRecord{ModelID: "mutated-model"}
	firstKey.KeyName = "mutated-key"
	if firstKey.KeyName != "mutated-key" {
		t.Fatalf("expected first proxy key clone to be locally mutable, got %+v", firstKey)
	}
	if firstKey.ExpiresAt != nil {
		mutatedExpiry := firstKey.ExpiresAt.Add(10 * time.Minute)
		firstKey.ExpiresAt = &mutatedExpiry
	}

	secondProfile, err := cache.LoadPublishedActiveProfile()
	if err != nil {
		t.Fatalf("reload published active profile: %v", err)
	}
	if secondProfile.Description == nil || *secondProfile.Description != "published-active description" {
		t.Fatalf("expected active profile clone to remain immutable, got %+v", secondProfile)
	}

	secondPlanning, err := cache.LoadPublishedPlanningSnapshot(42)
	if err != nil {
		t.Fatalf("reload published planning snapshot: %v", err)
	}
	if secondPlanning != firstPlanning {
		t.Fatalf("expected published planning snapshot reads to share the compiled pointer, got first=%p second=%p", firstPlanning, secondPlanning)
	}
	if secondPlanning.ReportCurrency.Code != "mutated-code" {
		t.Fatalf("expected published planning snapshot reads to expose shared state, got %+v", secondPlanning.ReportCurrency)
	}
	if secondPlanning.ModelsByID["published-planning"].ModelID != "mutated-model" {
		t.Fatalf("expected shared planning snapshot model mutation to remain visible, got %+v", secondPlanning.ModelsByID)
	}

	secondKey, ok, err := cache.LoadRuntimeProxyKeyRecord("pm-12345678")
	if err != nil {
		t.Fatalf("reload published proxy key record: %v", err)
	}
	if !ok {
		t.Fatal("expected published proxy key record to still exist")
	}
	if secondKey.KeyName != "published-key" {
		t.Fatalf("expected proxy key record clone to remain immutable, got %+v", secondKey)
	}
	if secondKey.ExpiresAt == nil || !secondKey.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected proxy key expiry clone to remain immutable, got %+v", secondKey.ExpiresAt)
	}

	authSettings, err := cache.LoadRuntimeAuthSettings()
	if err != nil {
		t.Fatalf("load published runtime auth settings: %v", err)
	}
	if !authSettings.AuthEnabled {
		t.Fatalf("expected published runtime auth settings to survive reads, got %+v", authSettings)
	}
}

func TestSharedCachePublishedGenerationTracksStores(t *testing.T) {
	t.Parallel()

	cache := NewSharedCache(0)
	if got := cache.PublishedGeneration(); got != 0 {
		t.Fatalf("expected empty cache generation 0, got %d", got)
	}

	cache.published.Store(&publishedRuntimeSnapshot{Generation: 3})
	if got := cache.PublishedGeneration(); got != 3 {
		t.Fatalf("expected published cache generation 3, got %d", got)
	}
	if !cache.PublishedReady() {
		t.Fatal("expected published cache to report ready after publication")
	}
}

func TestRefreshRequestNormalizationAndMerge(t *testing.T) {
	t.Parallel()

	normalized := (RefreshRequest{Auth: true, PlanningProfileIDs: []int{0, 4, 4, 2}}).normalized()
	if !normalized.Auth {
		t.Fatal("expected normalized refresh request to keep auth scope")
	}
	if !reflect.DeepEqual(normalized.PlanningProfileIDs, []int{2, 4}) {
		t.Fatalf("expected normalized planning ids [2 4], got %v", normalized.PlanningProfileIDs)
	}

	merged := mergeRefreshRequests(
		RefreshRequest{PlanningProfileIDs: []int{7, 2}},
		RefreshRequest{PlanningAll: true, ActiveProfile: true, PlanningProfileIDs: []int{1}},
	)
	if !merged.PlanningAll || !merged.ActiveProfile {
		t.Fatalf("expected merged request to keep broad scopes, got %+v", merged)
	}
	if len(merged.PlanningProfileIDs) != 0 {
		t.Fatalf("expected planning-all merge to clear specific ids, got %v", merged.PlanningProfileIDs)
	}
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
