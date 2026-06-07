package runtime

import "testing"

func TestRuntimeGenerationBumpsForRefresh(t *testing.T) {
	t.Parallel()

	bumps := RuntimeGenerationBumpsForRefresh(RefreshRequest{Auth: true, ActiveProfile: true, PlanningProfileIDs: []int{7, 7}}, "test")
	got := map[string]bool{}
	for _, bump := range bumps {
		got[bump.Scope.key()] = true
		if bump.Reason != "test" {
			t.Fatalf("expected bump reason test, got %q", bump.Reason)
		}
	}
	for _, key := range []string{
		"auth:global:*",
		"profile_runtime:global:*",
		"runtime_planning:global:*",
		"profile_runtime:profile:7",
		"runtime_planning:profile:7",
	} {
		if !got[key] {
			t.Fatalf("expected generation bump %q, got %+v", key, got)
		}
	}
	if len(got) != 5 {
		t.Fatalf("expected deduplicated generation bumps, got %+v", got)
	}
}

func TestRuntimeGenerationVectorFreshnessRequiresEveryScope(t *testing.T) {
	t.Parallel()

	scopes := DefaultRuntimeGenerationScopes()
	fresh := RuntimeGenerationVector{}
	for _, scope := range scopes {
		fresh[scope.key()] = 3
	}
	if !runtimeGenerationVectorsEqual(fresh, cloneRuntimeGenerationVector(fresh), scopes) {
		t.Fatal("expected identical generation vectors to be fresh")
	}
	missing := cloneRuntimeGenerationVector(fresh)
	delete(missing, GlobalRuntimeGenerationScope(RuntimeGenerationDomainAuth).key())
	if runtimeGenerationVectorsEqual(missing, fresh, scopes) {
		t.Fatal("expected missing auth generation to be stale")
	}
	stale := cloneRuntimeGenerationVector(fresh)
	stale[GlobalRuntimeGenerationScope(RuntimeGenerationDomainModelCatalog).key()] = 2
	if runtimeGenerationVectorsEqual(stale, fresh, scopes) {
		t.Fatal("expected lower model catalog generation to be stale")
	}
}

func TestRuntimeGenerationVectorTokenIsDeterministic(t *testing.T) {
	t.Parallel()

	left := RuntimeGenerationVector{
		"runtime_planning:global:*": 4,
		"profile_runtime:global:*":  2,
		"model_catalog:global:*":    8,
	}
	right := RuntimeGenerationVector{
		"model_catalog:global:*":    8,
		"profile_runtime:global:*":  2,
		"runtime_planning:global:*": 4,
	}
	if runtimeGenerationVectorToken(left) != runtimeGenerationVectorToken(right) {
		t.Fatal("expected generation vector token to ignore map iteration order")
	}

	changed := cloneRuntimeGenerationVector(left)
	changed["runtime_planning:global:*"] = 5
	if runtimeGenerationVectorToken(left) == runtimeGenerationVectorToken(changed) {
		t.Fatal("expected generation vector token to change when planning generation changes")
	}
	if runtimeGenerationVectorToken(nil) != runtimeGenerationVectorToken(RuntimeGenerationVector{}) {
		t.Fatal("expected nil and empty generation vectors to share the empty token")
	}
}
