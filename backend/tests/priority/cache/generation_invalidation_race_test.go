package cache

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerationInvalidationRace(t *testing.T) {
	backendRoot := backendRoot(t)
	cacheSource := readSource(t, filepath.Join(backendRoot, "internal", "httpapi", "runtime", "cache.go"))
	generationSource := readSource(t, filepath.Join(backendRoot, "internal", "httpapi", "runtime", "generations.go"))
	pgxutilSource := readSource(t, filepath.Join(backendRoot, "internal", "pgxutil", "tx.go"))
	middlewareSource := readSource(t, filepath.Join(backendRoot, "internal", "platform", "http", "runtime_cache_invalidation.go"))
	migrationSource := readSource(t, filepath.Join(backendRoot, "migrations", "000001_initial_schema.sql"))

	for _, want := range []string{
		"runtime_cache_generations",
		"auth",
		"runtime_planning",
		"profile_runtime",
		"model_catalog",
	} {
		if !strings.Contains(migrationSource, want) {
			t.Fatalf("runtime cache generation migration missing %q", want)
		}
	}
	for _, want := range []string{
		"ReadRuntimeGenerationVector(ctx, tx, DefaultRuntimeGenerationScopes())",
		"ErrRuntimeSnapshotGenerationChanged",
		"LoadFreshRuntimeAuthSettings",
		"LoadFreshRuntimeProxyKeyRecord",
		"LoadFreshActiveRuntimePlan",
	} {
		if !strings.Contains(cacheSource, want) && !strings.Contains(generationSource, want) {
			t.Fatalf("runtime cache generation implementation missing %q", want)
		}
	}
	for _, want := range []string{"WithBeforeCommitHook", "hook(ctx, tx)", "tx.Commit(ctx)"} {
		if !strings.Contains(pgxutilSource, want) {
			t.Fatalf("transaction hook support missing %q", want)
		}
	}
	if hookIndex := strings.Index(pgxutilSource, "hook(ctx, tx)"); hookIndex < 0 || hookIndex > strings.Index(pgxutilSource, "tx.Commit(ctx)") {
		t.Fatal("expected runtime generation hook to run before transaction commit")
	}
	for _, want := range []string{"RuntimeGenerationBumpsForRefresh", "AdvanceRuntimeCacheGenerations", "ScheduleRefresh(request)"} {
		if !strings.Contains(middlewareSource, want) {
			t.Fatalf("runtime cache invalidation middleware missing %q", want)
		}
	}
	for _, forbidden := range []string{"RefreshNow(ctx, request)", "failed to publish runtime auth snapshot immediately"} {
		if strings.Contains(middlewareSource, forbidden) {
			t.Fatalf("runtime cache invalidation middleware is still authoritative via %q", forbidden)
		}
	}
}

func backendRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func readSource(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
