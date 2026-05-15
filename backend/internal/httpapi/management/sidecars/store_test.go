package sidecars

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

const sidecarStoreSecretKey = "sidecar-store-test-secret-key"

var sidecarStorePostgres struct {
	once          sync.Once
	containerName string
	hostPort      string
	err           error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sidecarStorePostgres.containerName != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", sidecarStorePostgres.containerName).Run()
		cancel()
	}
	os.Exit(code)
}

func TestSidecarInstanceCanonicalURLUnique(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "canonical_url_unique")
	first := createTestSidecar(t, ctx, store, "canonical-a")
	_, err := store.CreateSidecarInstance(ctx, SidecarInstanceInput{
		Name:               "Canonical duplicate",
		BaseURL:            "https://duplicate.example.test/",
		BaseURLCanonical:   first.BaseURLCanonical,
		ManagementPassword: "duplicate-password",
	})
	if !IsStoreError(err, StoreErrorDuplicateSidecarCanonicalURL) {
		t.Fatalf("expected duplicate canonical URL error, got %v", err)
	}
	deleted, err := store.SoftDeleteSidecarInstance(ctx, first.ID)
	if err != nil || !deleted {
		t.Fatalf("soft delete first sidecar: deleted=%v err=%v", deleted, err)
	}
	created, err := store.CreateSidecarInstance(ctx, SidecarInstanceInput{
		Name:               "Canonical duplicate after delete",
		BaseURL:            "https://duplicate.example.test/",
		BaseURLCanonical:   first.BaseURLCanonical,
		ManagementPassword: "duplicate-password",
	})
	if err != nil {
		t.Fatalf("expected duplicate canonical URL after soft delete to insert: %v", err)
	}
	if created.ID == first.ID {
		t.Fatalf("expected replacement sidecar to get a new row id")
	}
}

func TestSidecarInstanceNameUniqueAndNetworkDefaults(t *testing.T) {
	ctx, store, pool := sidecarStoreForTest(t, "name_unique_defaults")
	var allowPrivate, allowHTTP, skipTLS bool
	err := pool.QueryRow(ctx, `INSERT INTO sidecar_instances (name, base_url, base_url_canonical, management_password)
VALUES ($1, $2, $3, $4)
RETURNING allow_private_network, allow_insecure_http, skip_tls_verify`,
		"default flags", "https://defaults.example.test", "https://defaults.example.test", "enc:fixture",
	).Scan(&allowPrivate, &allowHTTP, &skipTLS)
	if err != nil {
		t.Fatalf("insert sidecar defaults fixture: %v", err)
	}
	if allowPrivate || allowHTTP || skipTLS {
		t.Fatalf("network policy defaults must be false, got private=%v http=%v tls=%v", allowPrivate, allowHTTP, skipTLS)
	}
	first := createTestSidecar(t, ctx, store, "name-a")
	_, err = store.CreateSidecarInstance(ctx, SidecarInstanceInput{
		Name:               strings.ToLower(first.Name),
		BaseURL:            "https://name-duplicate.example.test/",
		BaseURLCanonical:   "https://name-duplicate.example.test",
		ManagementPassword: "duplicate-password",
	})
	if !IsStoreError(err, StoreErrorDuplicateSidecarName) {
		t.Fatalf("expected duplicate case-insensitive name error, got %v", err)
	}
	if _, err := store.SoftDeleteSidecarInstance(ctx, first.ID); err != nil {
		t.Fatalf("soft delete first sidecar: %v", err)
	}
	_, err = store.CreateSidecarInstance(ctx, SidecarInstanceInput{
		Name:               strings.ToLower(first.Name),
		BaseURL:            "https://name-duplicate.example.test/",
		BaseURLCanonical:   "https://name-duplicate.example.test",
		ManagementPassword: "duplicate-password",
	})
	if err != nil {
		t.Fatalf("expected duplicate name after soft delete to insert: %v", err)
	}
}

func TestSidecarManagementPasswordEncrypted(t *testing.T) {
	ctx, store, pool := sidecarStoreForTest(t, "management_password_encrypted")
	rawPassword := "fixture-management-password"
	created, err := store.CreateSidecarInstance(ctx, SidecarInstanceInput{
		Name:               "Encrypted password",
		BaseURL:            "https://encrypted.example.test/",
		BaseURLCanonical:   "https://encrypted.example.test",
		ManagementPassword: rawPassword,
	})
	if err != nil {
		t.Fatalf("create sidecar: %v", err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT management_password FROM sidecar_instances WHERE id = $1`, created.ID).Scan(&stored); err != nil {
		t.Fatalf("load stored management password: %v", err)
	}
	if stored == rawPassword || strings.Contains(stored, rawPassword) {
		t.Fatalf("management password must not be stored in plaintext, got %q", stored)
	}
	if !strings.HasPrefix(stored, "enc:") {
		t.Fatalf("expected Prism encrypted secret prefix, got %q", stored)
	}
	decrypted, err := endpointdomain.DecryptSecret(stored, sidecarStoreSecretKey)
	if err != nil {
		t.Fatalf("decrypt stored management password: %v", err)
	}
	if decrypted != rawPassword {
		t.Fatalf("decrypted management password = %q, want %q", decrypted, rawPassword)
	}
	_, err = store.CreateSidecarInstance(ctx, SidecarInstanceInput{
		Name:               "Reserved prefix password",
		BaseURL:            "https://reserved-prefix.example.test/",
		BaseURLCanonical:   "https://reserved-prefix.example.test",
		ManagementPassword: "enc:raw-management-password",
	})
	if !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected raw reserved-prefix management password to be rejected, got %v", err)
	}
}

func TestSidecarInstanceCRUD(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "instance_crud")
	created := createTestSidecar(t, ctx, store, "crud")
	loaded, ok, err := store.GetSidecarInstance(ctx, created.ID)
	if err != nil || !ok {
		t.Fatalf("get sidecar instance: ok=%v err=%v", ok, err)
	}
	if loaded.Name != created.Name || loaded.BaseURLCanonical != created.BaseURLCanonical {
		t.Fatalf("loaded sidecar mismatch: got %+v want %+v", loaded, created)
	}
	instances, err := store.ListSidecarInstances(ctx)
	if err != nil {
		t.Fatalf("list sidecar instances: %v", err)
	}
	if len(instances) != 1 || instances[0].ID != created.ID {
		t.Fatalf("expected list to contain created sidecar, got %+v", instances)
	}
	enabled := false
	updated, err := store.UpdateSidecarInstance(ctx, created.ID, SidecarInstanceInput{Name: "CRUD updated", BaseURL: created.BaseURL, BaseURLCanonical: created.BaseURLCanonical, ManagementPassword: created.EncryptedManagementPassword, ManagementPasswordIsEncrypted: true, Enabled: &enabled, SyncIntervalSeconds: 600, RequestTimeoutSeconds: 20, ManagementAuthState: ManagementAuthStateInvalid})
	if err != nil {
		t.Fatalf("update sidecar instance: %v", err)
	}
	if updated.Enabled || updated.SyncIntervalSeconds != 600 || updated.ManagementAuthState != ManagementAuthStateInvalid {
		t.Fatalf("updated sidecar fields not persisted: %+v", updated)
	}
}

func TestSidecarSnapshotJSONPersistence(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "snapshot_json_persistence")
	sidecar := createTestSidecar(t, ctx, store, "snapshots")
	provider := "gemini"
	label := "primary"
	status := "ok"
	disabled := false
	priority := 20
	successCount := 7
	failedCount := 2
	observedAt := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	auth, err := store.SaveAuthSnapshot(ctx, SidecarAuthSnapshotInput{
		SidecarID:          sidecar.ID,
		AuthID:             "auth-gemini-primary",
		AuthIndex:          stringPtr("auth_001"),
		Name:               "gemini-primary.json",
		Provider:           &provider,
		Label:              &label,
		Status:             &status,
		Disabled:           &disabled,
		Priority:           &priority,
		SuccessCount:       &successCount,
		FailedCount:        &failedCount,
		RecentRequestsJSON: json.RawMessage(`[{"window":"1m","success":7}]`),
		ModelStatesJSON:    json.RawMessage(`{"gemini-pro":{"state":"ok"}}`),
		SnapshotJSON:       json.RawMessage(`{"source":"cliproxy","api_key":"redacted-fixture"}`),
		ObservedAt:         observedAt,
	})
	if err != nil {
		t.Fatalf("save auth snapshot: %v", err)
	}
	updatedStatus := "degraded"
	updated, err := store.SaveAuthSnapshot(ctx, SidecarAuthSnapshotInput{
		SidecarID:          sidecar.ID,
		AuthID:             "auth-gemini-primary",
		Name:               "gemini-primary.json",
		Status:             &updatedStatus,
		RecentRequestsJSON: json.RawMessage(`[]`),
		ModelStatesJSON:    json.RawMessage(`{}`),
		SnapshotJSON:       json.RawMessage(`{"source":"cliproxy-updated"}`),
		ObservedAt:         observedAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("update auth snapshot: %v", err)
	}
	if updated.ID != auth.ID {
		t.Fatalf("expected auth snapshot upsert to reuse id %d, got %d", auth.ID, updated.ID)
	}
	loaded, ok, err := store.GetAuthSnapshot(ctx, sidecar.ID, "auth-gemini-primary")
	if err != nil || !ok {
		t.Fatalf("load auth snapshot: ok=%v err=%v", ok, err)
	}
	requireJSONEqual(t, loaded.SnapshotJSON, `{"source":"cliproxy-updated"}`)
	authList, err := store.ListAuthSnapshots(ctx, sidecar.ID)
	if err != nil {
		t.Fatalf("list auth snapshots: %v", err)
	}
	if len(authList) != 1 || authList[0].ID != auth.ID {
		t.Fatalf("expected one listed auth snapshot, got %+v", authList)
	}
	_, err = store.SaveAuthSnapshot(ctx, SidecarAuthSnapshotInput{SidecarID: sidecar.ID, AuthID: "raw-secret", Name: "raw-secret.json", SnapshotJSON: json.RawMessage(`{"api-key":"raw-secret-value"}`)})
	if !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected raw auth snapshot secret to be rejected, got %v", err)
	}
	_, err = store.SaveAuthSnapshot(ctx, SidecarAuthSnapshotInput{SidecarID: sidecar.ID, AuthID: "raw-password", Name: "raw-password.json", SnapshotJSON: json.RawMessage(`{"metadata":{"password":"raw-password-value","management_key":"raw-management-key"}}`)})
	if !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected nested raw password/management key snapshot secret to be rejected, got %v", err)
	}
	providerSnapshot, err := store.SaveProviderSnapshot(ctx, SidecarProviderSnapshotInput{
		SidecarID:       sidecar.ID,
		ProviderKey:     "gemini-api-key",
		ProviderItemKey: "gemini-default",
		Name:            stringPtr("Gemini default"),
		Label:           stringPtr("Default"),
		Status:          stringPtr("configured"),
		Disabled:        boolPtr(false),
		SnapshotJSON:    json.RawMessage(`{"api-key":"redacted-fixture","priority":10}`),
		ObservedAt:      observedAt,
	})
	if err != nil {
		t.Fatalf("save provider snapshot: %v", err)
	}
	loadedProvider, ok, err := store.GetProviderSnapshot(ctx, sidecar.ID, "gemini-api-key", "gemini-default")
	if err != nil || !ok {
		t.Fatalf("load provider snapshot: ok=%v err=%v", ok, err)
	}
	if loadedProvider.ID != providerSnapshot.ID {
		t.Fatalf("loaded provider snapshot id = %d, want %d", loadedProvider.ID, providerSnapshot.ID)
	}
	requireJSONEqual(t, loadedProvider.SnapshotJSON, `{"api-key":"redacted-fixture","priority":10}`)
	providerList, err := store.ListProviderSnapshots(ctx, sidecar.ID)
	if err != nil {
		t.Fatalf("list provider snapshots: %v", err)
	}
	if len(providerList) != 1 || providerList[0].ID != providerSnapshot.ID {
		t.Fatalf("expected one listed provider snapshot, got %+v", providerList)
	}
	_, err = store.SaveProviderSnapshot(ctx, SidecarProviderSnapshotInput{SidecarID: sidecar.ID, ProviderKey: "claude-api-key", ProviderItemKey: "claude-default", SnapshotJSON: json.RawMessage(`{"api_key":"raw-secret-value"}`)})
	if !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected raw provider snapshot secret to be rejected, got %v", err)
	}
}

func TestReplaceAuthSnapshotsGenerationSemantics(t *testing.T) {
	for _, testCase := range authSnapshotReplacementStoreCases() {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.setup(t, "auth_replace_generation_"+testCase.name)
			sidecar := createTestSidecarInReplacementStore(t, ctx, store, "auth_replace_generation_"+testCase.name)
			if _, err := store.SaveAuthSnapshot(ctx, testAuthSnapshotInput(sidecar.ID, "auth-stale", "stale.json", 0)); err != nil {
				t.Fatalf("seed stale auth snapshot: %v", err)
			}
			if _, err := store.SaveAuthSnapshot(ctx, testAuthSnapshotInput(sidecar.ID, "auth-keep", "keep.json", 0)); err != nil {
				t.Fatalf("seed kept auth snapshot: %v", err)
			}

			replaced, err := store.ReplaceAuthSnapshots(ctx, sidecar.ID, []SidecarAuthSnapshotInput{
				testAuthSnapshotInput(sidecar.ID, "auth-keep", "keep.json", 1),
				testAuthSnapshotInput(sidecar.ID, "auth-new", "new.json", 1),
			})
			if err != nil {
				t.Fatalf("replace auth snapshots: %v", err)
			}
			if len(replaced) != 2 {
				t.Fatalf("expected two replacement records, got %+v", replaced)
			}
			listed, err := store.ListAuthSnapshots(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("list auth snapshots after replacement: %v", err)
			}
			byAuthID := requireAuthSnapshotSet(t, listed, "auth-keep", "auth-new")
			requireJSONEqual(t, byAuthID["auth-keep"].SnapshotJSON, `{"generation":1}`)

			if _, err := store.ReplaceAuthSnapshots(ctx, sidecar.ID, []SidecarAuthSnapshotInput{
				testAuthSnapshotInput(sidecar.ID, "auth-keep", "keep.json", 2),
			}); err != nil {
				t.Fatalf("repeat auth snapshot replacement: %v", err)
			}
			listed, err = store.ListAuthSnapshots(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("list auth snapshots after repeated replacement: %v", err)
			}
			byAuthID = requireAuthSnapshotSet(t, listed, "auth-keep")
			requireJSONEqual(t, byAuthID["auth-keep"].SnapshotJSON, `{"generation":2}`)
		})
	}
}

func TestReplaceAuthSnapshotsEmptyGenerationClearsRows(t *testing.T) {
	for _, testCase := range authSnapshotReplacementStoreCases() {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.setup(t, "auth_replace_empty_"+testCase.name)
			sidecar := createTestSidecarInReplacementStore(t, ctx, store, "auth_replace_empty_"+testCase.name)
			if _, err := store.SaveAuthSnapshot(ctx, testAuthSnapshotInput(sidecar.ID, "auth-a", "a.json", 0)); err != nil {
				t.Fatalf("seed first auth snapshot: %v", err)
			}
			if _, err := store.SaveAuthSnapshot(ctx, testAuthSnapshotInput(sidecar.ID, "auth-b", "b.json", 0)); err != nil {
				t.Fatalf("seed second auth snapshot: %v", err)
			}

			replaced, err := store.ReplaceAuthSnapshots(ctx, sidecar.ID, nil)
			if err != nil {
				t.Fatalf("replace with empty auth generation: %v", err)
			}
			if len(replaced) != 0 {
				t.Fatalf("expected empty replacement result, got %+v", replaced)
			}
			listed, err := store.ListAuthSnapshots(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("list auth snapshots after empty replacement: %v", err)
			}
			if len(listed) != 0 {
				t.Fatalf("expected empty generation to clear rows, got %+v", listed)
			}
		})
	}
}

func TestReplaceAuthSnapshotsValidationPreservesPreviousGeneration(t *testing.T) {
	for _, testCase := range authSnapshotReplacementStoreCases() {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.setup(t, "auth_replace_validate_"+testCase.name)
			sidecar := createTestSidecarInReplacementStore(t, ctx, store, "auth_replace_validate_"+testCase.name)
			if _, err := store.SaveAuthSnapshot(ctx, testAuthSnapshotInput(sidecar.ID, "auth-old", "old.json", 0)); err != nil {
				t.Fatalf("seed previous auth snapshot: %v", err)
			}

			_, err := store.ReplaceAuthSnapshots(ctx, sidecar.ID, []SidecarAuthSnapshotInput{
				testAuthSnapshotInput(sidecar.ID, "auth-new", "new.json", 1),
				{SidecarID: sidecar.ID, AuthID: "auth-invalid", Name: "invalid.json", SnapshotJSON: json.RawMessage(`{"api_key":"raw-secret-value"}`)},
			})
			if !IsStoreError(err, StoreErrorInvalidInput) {
				t.Fatalf("expected invalid replacement input error, got %v", err)
			}
			listed, err := store.ListAuthSnapshots(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("list auth snapshots after invalid replacement: %v", err)
			}
			byAuthID := requireAuthSnapshotSet(t, listed, "auth-old")
			requireJSONEqual(t, byAuthID["auth-old"].SnapshotJSON, `{"generation":0}`)
		})
	}
}

func TestReplaceAuthSnapshotsPostgresRollbackPreservesPreviousGeneration(t *testing.T) {
	ctx, store, pool := sidecarStoreForTest(t, "auth_replace_rollback")
	sidecar := createTestSidecar(t, ctx, store, "auth_replace_rollback")
	if _, err := store.ReplaceAuthSnapshots(ctx, sidecar.ID, []SidecarAuthSnapshotInput{testAuthSnapshotInput(sidecar.ID, "auth-old", "old.json", 0)}); err != nil {
		t.Fatalf("seed previous auth generation: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE OR REPLACE FUNCTION reject_sidecar_auth_snapshot_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
	IF NEW.name = 'reject-auth.json' THEN
		RAISE EXCEPTION 'forced auth replacement failure';
	END IF;
	RETURN NEW;
END;
$$`); err != nil {
		t.Fatalf("create auth replacement rejection function: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER reject_sidecar_auth_snapshot_insert
BEFORE INSERT ON sidecar_auth_snapshots
FOR EACH ROW EXECUTE FUNCTION reject_sidecar_auth_snapshot_insert()`); err != nil {
		t.Fatalf("create auth replacement rejection trigger: %v", err)
	}

	_, err := store.ReplaceAuthSnapshots(ctx, sidecar.ID, []SidecarAuthSnapshotInput{testAuthSnapshotInput(sidecar.ID, "auth-new", "reject-auth.json", 1)})
	if err == nil {
		t.Fatal("expected replacement insert failure")
	}
	listed, err := store.ListAuthSnapshots(ctx, sidecar.ID)
	if err != nil {
		t.Fatalf("list auth snapshots after failed replacement: %v", err)
	}
	byAuthID := requireAuthSnapshotSet(t, listed, "auth-old")
	requireJSONEqual(t, byAuthID["auth-old"].SnapshotJSON, `{"generation":0}`)
}

type authSnapshotReplacementTestStore interface {
	CreateSidecarInstance(context.Context, SidecarInstanceInput) (SidecarInstance, error)
	SaveAuthSnapshot(context.Context, SidecarAuthSnapshotInput) (SidecarAuthSnapshot, error)
	ReplaceAuthSnapshots(context.Context, int, []SidecarAuthSnapshotInput) ([]SidecarAuthSnapshot, error)
	ListAuthSnapshots(context.Context, int) ([]SidecarAuthSnapshot, error)
}

type authSnapshotReplacementStoreCase struct {
	name  string
	setup func(*testing.T, string) (context.Context, authSnapshotReplacementTestStore)
}

func authSnapshotReplacementStoreCases() []authSnapshotReplacementStoreCase {
	return []authSnapshotReplacementStoreCase{
		{
			name: "postgres",
			setup: func(t *testing.T, name string) (context.Context, authSnapshotReplacementTestStore) {
				ctx, store, _ := sidecarStoreForTest(t, name)
				return ctx, store
			},
		},
		{
			name: "memory",
			setup: func(t *testing.T, _ string) (context.Context, authSnapshotReplacementTestStore) {
				t.Helper()
				return context.Background(), newMemorySidecarStore(func() time.Time {
					return time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
				}, sidecarStoreSecretKey)
			},
		},
	}
}

func createTestSidecarInReplacementStore(t *testing.T, ctx context.Context, store authSnapshotReplacementTestStore, suffix string) SidecarInstance {
	t.Helper()
	host := strings.ReplaceAll(suffix, "_", "-") + ".example.test"
	created, err := store.CreateSidecarInstance(ctx, SidecarInstanceInput{
		Name:               "Sidecar " + suffix,
		BaseURL:            "https://" + host + "/",
		BaseURLCanonical:   "https://" + host,
		ManagementPassword: "password-" + suffix,
	})
	if err != nil {
		t.Fatalf("create test sidecar %s: %v", suffix, err)
	}
	return created
}

func testAuthSnapshotInput(sidecarID int, authID string, name string, generation int) SidecarAuthSnapshotInput {
	return SidecarAuthSnapshotInput{
		SidecarID:    sidecarID,
		AuthID:       authID,
		Name:         name,
		Status:       stringPtr(fmt.Sprintf("generation-%d", generation)),
		SnapshotJSON: json.RawMessage(fmt.Sprintf(`{"generation":%d}`, generation)),
	}
}

func requireAuthSnapshotSet(t *testing.T, snapshots []SidecarAuthSnapshot, wantAuthIDs ...string) map[string]SidecarAuthSnapshot {
	t.Helper()
	if len(snapshots) != len(wantAuthIDs) {
		t.Fatalf("auth snapshot count = %d, want %d: %+v", len(snapshots), len(wantAuthIDs), snapshots)
	}
	byAuthID := make(map[string]SidecarAuthSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		if _, exists := byAuthID[snapshot.AuthID]; exists {
			t.Fatalf("duplicate auth snapshot for auth_id %s in %+v", snapshot.AuthID, snapshots)
		}
		byAuthID[snapshot.AuthID] = snapshot
	}
	for _, authID := range wantAuthIDs {
		if _, exists := byAuthID[authID]; !exists {
			t.Fatalf("missing auth snapshot %s in %+v", authID, snapshots)
		}
	}
	return byAuthID
}

func TestSidecarWatchdogPersistenceContracts(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "watchdog_persistence")
	sidecar := createTestSidecar(t, ctx, store, "watchdog")
	policy, err := store.GetOrCreateWatchdogPolicy(ctx, sidecar.ID)
	if err != nil {
		t.Fatalf("get default watchdog policy: %v", err)
	}
	if policy.Enabled || policy.QuotaExceededPriority != DefaultQuotaExceededPriority || policy.WorkingPriority != DefaultWorkingPriority || policy.EmptyQuotaPriority != DefaultEmptyQuotaPriority || policy.InitialPriority != DefaultInitialPriority || policy.ErrorPriority != DefaultErrorPriority {
		t.Fatalf("unexpected default policy: %+v", policy)
	}
	policy, err = store.UpsertWatchdogPolicy(ctx, SidecarWatchdogPolicyInput{
		SidecarID:                  sidecar.ID,
		Enabled:                    true,
		FailureThreshold:           5,
		FailureWindowSeconds:       7200,
		FallbackCooldownSeconds:    3600,
		QuotaExceededPriority:      0,
		ManualOverridePauseSeconds: 900,
	})
	if err != nil {
		t.Fatalf("upsert watchdog policy: %v", err)
	}
	if !policy.Enabled || policy.FailureThreshold != 5 || policy.ManualOverridePauseSeconds != 900 {
		t.Fatalf("custom policy not persisted: %+v", policy)
	}
	previousPriority := 20
	holdUntil := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)
	hold, err := store.CreateWatchdogHold(ctx, SidecarWatchdogHoldInput{
		SidecarID:        sidecar.ID,
		AuthID:           "auth-watchdog-primary",
		AuthIndex:        stringPtr("auth_002"),
		Provider:         stringPtr("claude"),
		Reason:           "quota_exceeded",
		ConditionHash:    "hash-quota-exceeded",
		PreviousPriority: &previousPriority,
		TargetPriority:   0,
		HoldUntil:        &holdUntil,
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create watchdog hold: %v", err)
	}
	_, err = store.CreateWatchdogHold(ctx, SidecarWatchdogHoldInput{
		SidecarID:      sidecar.ID,
		AuthID:         "auth-watchdog-primary",
		Reason:         "manual_pause",
		ConditionHash:  "hash-manual",
		TargetPriority: 0,
		Status:         "paused",
	})
	if !IsStoreError(err, StoreErrorDuplicateActiveHold) {
		t.Fatalf("expected duplicate active/paused hold error, got %v", err)
	}
	_, err = store.CreateWatchdogHold(ctx, SidecarWatchdogHoldInput{
		SidecarID:      sidecar.ID,
		AuthID:         "auth-watchdog-primary",
		Reason:         "released_history",
		ConditionHash:  "hash-released",
		TargetPriority: 0,
		Status:         "released",
		ReleasedAt:     timePtr(holdUntil),
	})
	if err != nil {
		t.Fatalf("released hold history should not conflict with active hold: %v", err)
	}
	action, err := store.CreateWatchdogAction(ctx, SidecarWatchdogActionInput{
		SidecarID:        sidecar.ID,
		HoldID:           &hold.ID,
		AuthID:           stringPtr("auth-watchdog-primary"),
		AuthName:         stringPtr("auth-watchdog-primary.json"),
		AuthIndex:        stringPtr("auth_002"),
		Provider:         stringPtr("claude"),
		ActionType:       "deprioritize",
		Reason:           stringPtr("quota_exceeded"),
		PreviousPriority: &previousPriority,
		TargetPriority:   intPtr(0),
		HoldUntil:        &holdUntil,
		Status:           "succeeded",
		CompletedAt:      timePtr(holdUntil.Add(time.Minute)),
	})
	if err != nil {
		t.Fatalf("create watchdog action: %v", err)
	}
	if action.HoldID == nil || *action.HoldID != hold.ID || action.TargetPriority == nil || *action.TargetPriority != 0 || stringValue(action.AuthName) != "auth-watchdog-primary.json" {
		t.Fatalf("watchdog action did not persist hold/priority/auth-name fields: %+v", action)
	}
	actions, err := store.ListWatchdogActions(ctx, sidecar.ID)
	if err != nil {
		t.Fatalf("list watchdog actions: %v", err)
	}
	if len(actions) != 1 || actions[0].ID != action.ID || stringValue(actions[0].AuthName) != "auth-watchdog-primary.json" {
		t.Fatalf("expected one listed watchdog action with auth name, got %+v", actions)
	}
}

func sidecarStoreForTest(t *testing.T, name string) (context.Context, *Store, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	pool := sidecarMigratedPool(t, ctx, name)
	store := NewStore(StoreOptions{
		Pool:                pool,
		Now:                 func() time.Time { return time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC) },
		SecretEncryptionKey: sidecarStoreSecretKey,
	})
	return ctx, store, pool
}

func sidecarMigratedPool(t *testing.T, ctx context.Context, name string) *pgxpool.Pool {
	t.Helper()
	harness := sidecarPostgresHarness(t)
	databaseName := fmt.Sprintf("%s_%s", name, sidecarRandomSuffix(t))
	conn := harness.openDatabase(t, ctx, databaseName)
	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for %s: %v", databaseName, err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close migration connection: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open sidecar store pool: %v", err)
	}
	retentionStore := logretention.NewStore(logretention.Options{Pool: pool})
	if err := retentionStore.EnsurePartitionHorizonForTable(ctx, "sidecar_watchdog_actions"); err != nil {
		t.Fatalf("ensure sidecar action history partitions: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type sidecarPostgresTestHarness struct {
	containerName string
	hostPort      string
}

func sidecarPostgresHarness(t *testing.T) sidecarPostgresTestHarness {
	t.Helper()
	sidecarStorePostgres.once.Do(func() {
		containerName := "prism-sidecars-" + sidecarRandomSuffix(t)
		if _, err := runSidecarDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			sidecarStorePostgres.err = err
			return
		}
		sidecarStorePostgres.containerName = containerName
		hostPort, err := sidecarDockerPort(containerName)
		if err != nil {
			sidecarStorePostgres.err = err
			return
		}
		if err := waitForSidecarPostgres(hostPort); err != nil {
			sidecarStorePostgres.err = err
			return
		}
		sidecarStorePostgres.hostPort = hostPort
	})
	if sidecarStorePostgres.err != nil {
		t.Fatalf("start postgres harness: %v", sidecarStorePostgres.err)
	}
	return sidecarPostgresTestHarness{
		containerName: sidecarStorePostgres.containerName,
		hostPort:      sidecarStorePostgres.hostPort,
	}
}

func (h sidecarPostgresTestHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := sidecarConnect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+sidecarQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+sidecarQuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return sidecarConnect(t, ctx, h.connectionString(databaseName))
}

func (h sidecarPostgresTestHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func sidecarConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func sidecarDockerPort(containerName string) (string, error) {
	output, err := runSidecarDockerCommand(context.Background(), "port", containerName, "5432/tcp")
	if err != nil {
		return "", err
	}
	firstLine := strings.TrimSpace(strings.Split(output, "\n")[0])
	_, port, err := net.SplitHostPort(firstLine)
	if err != nil {
		return "", fmt.Errorf("parse docker port output %q: %w", firstLine, err)
	}
	return port, nil
}

func waitForSidecarPostgres(hostPort string) error {
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/postgres?sslmode=disable", hostPort))
		if err == nil {
			_ = conn.Close(ctx)
			cancel()
			return nil
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("postgres container on port %s did not become ready in time", hostPort)
}

func runSidecarDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func sidecarQuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func sidecarRandomSuffix(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(buffer)
}

func createTestSidecar(t *testing.T, ctx context.Context, store *Store, suffix string) SidecarInstance {
	t.Helper()
	host := strings.ReplaceAll(suffix, "_", "-") + ".example.test"
	created, err := store.CreateSidecarInstance(ctx, SidecarInstanceInput{
		Name:               "Sidecar " + suffix,
		BaseURL:            "https://" + host + "/",
		BaseURLCanonical:   "https://" + host,
		ManagementPassword: "password-" + suffix,
	})
	if err != nil {
		t.Fatalf("create test sidecar %s: %v", suffix, err)
	}
	return created
}

func requireJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var compactGot bytes.Buffer
	if err := json.Compact(&compactGot, got); err != nil {
		t.Fatalf("compact got JSON %s: %v", string(got), err)
	}
	var compactWant bytes.Buffer
	if err := json.Compact(&compactWant, []byte(want)); err != nil {
		t.Fatalf("compact want JSON %s: %v", want, err)
	}
	if compactGot.String() != compactWant.String() {
		t.Fatalf("JSON mismatch got %s want %s", compactGot.String(), compactWant.String())
	}
}

func requireJSONEquivalent(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("unmarshal got JSON %s: %v", string(got), err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("unmarshal want JSON %s: %v", want, err)
	}
	gotCanonical, err := json.Marshal(gotValue)
	if err != nil {
		t.Fatalf("canonicalize got JSON: %v", err)
	}
	wantCanonical, err := json.Marshal(wantValue)
	if err != nil {
		t.Fatalf("canonicalize want JSON: %v", err)
	}
	if string(gotCanonical) != string(wantCanonical) {
		t.Fatalf("JSON mismatch got %s want %s", gotCanonical, wantCanonical)
	}
}

func stringPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }

func intPtr(value int) *int { return &value }

func timePtr(value time.Time) *time.Time { return &value }

func TestWatchdogPolicyProbeFieldsRoundTripAndValidation(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "policy_probe_fields") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "policy_probe_"+testCase.name)
			policy, err := store.GetOrCreateWatchdogPolicy(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("get default policy: %v", err)
			}
			if policy.UsingPriority != DefaultUsingPriority || policy.ProbeConcurrency != DefaultProbeConcurrency || policy.ProbeTimeoutSeconds != DefaultProbeTimeoutSeconds {
				t.Fatalf("unexpected default probe policy fields: %+v", policy)
			}
			updated, err := store.UpsertWatchdogPolicy(ctx, SidecarWatchdogPolicyInput{
				SidecarID: sidecar.ID, Enabled: true, FailureThreshold: 5,
				FailureWindowSeconds: 7200, FallbackCooldownSeconds: 3600,
				QuotaExceededPriority: 2, UsingPriority: 5,
				ManualOverridePauseSeconds: 900, ProbeConcurrency: 2, ProbeTimeoutSeconds: 10,
			})
			if err != nil {
				t.Fatalf("upsert custom probe policy: %v", err)
			}
			if !updated.Enabled || updated.QuotaExceededPriority != 2 || updated.UsingPriority != 5 || updated.ProbeConcurrency != 2 || updated.ProbeTimeoutSeconds != 10 {
				t.Fatalf("custom probe policy not persisted: %+v", updated)
			}
			reloaded, err := store.GetOrCreateWatchdogPolicy(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("reload custom probe policy: %v", err)
			}
			if reloaded.UsingPriority != 5 || reloaded.ProbeConcurrency != 2 || reloaded.ProbeTimeoutSeconds != 10 || reloaded.ProbeBatchCooldownSeconds != DefaultProbeBatchCooldownSeconds || reloaded.ProbeLastBatchCompletedAt != nil {
				t.Fatalf("custom probe policy did not round-trip: %+v", reloaded)
			}
			visibleUpdate, err := store.UpsertWatchdogPolicy(ctx, SidecarWatchdogPolicyInput{
				SidecarID: sidecar.ID, Enabled: false, FailureThreshold: 6,
				FailureWindowSeconds: 1800, FallbackCooldownSeconds: 2400,
				QuotaExceededPriority: 3, ManualOverridePauseSeconds: 1200,
			})
			if err != nil {
				t.Fatalf("upsert visible-only policy fields: %v", err)
			}
			if visibleUpdate.UsingPriority != 5 || visibleUpdate.ProbeConcurrency != 2 || visibleUpdate.ProbeTimeoutSeconds != 10 {
				t.Fatalf("visible-only policy update clobbered hidden probe fields: %+v", visibleUpdate)
			}
			singleConcurrency, err := store.UpsertWatchdogPolicy(ctx, SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, QuotaExceededPriority: 0, UsingPriority: 5, ProbeConcurrency: 1, ProbeTimeoutSeconds: 1})
			if err != nil {
				t.Fatalf("expected probe_concurrency=1 to be valid, got %v", err)
			}
			if singleConcurrency.ProbeConcurrency != 1 {
				t.Fatalf("probe_concurrency=1 did not persist: %+v", singleConcurrency)
			}
			_, err = store.UpsertWatchdogPolicy(ctx, SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, QuotaExceededPriority: 3, UsingPriority: 2, ProbeConcurrency: 1, ProbeTimeoutSeconds: 1})
			if !IsStoreError(err, StoreErrorInvalidInput) {
				t.Fatalf("expected invalid priority policy error, got %v", err)
			}
			_, err = store.UpsertWatchdogPolicy(ctx, SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, QuotaExceededPriority: 0, UsingPriority: 5, ProbeConcurrency: MaxProbeConcurrency + 1, ProbeTimeoutSeconds: 1})
			if !IsStoreError(err, StoreErrorInvalidInput) {
				t.Fatalf("expected invalid probe concurrency policy error, got %v", err)
			}
			maxProbeTimeoutSeconds := watchdogProbeConcurrencyBudgetMaxSeconds()
			if maxProbeTimeoutSeconds != 120 {
				t.Fatalf("expected watchdog probe timeout budget to be 120s, got %ds", maxProbeTimeoutSeconds)
			}
			parallelBudget, err := store.UpsertWatchdogPolicy(ctx, SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, QuotaExceededPriority: 0, UsingPriority: 5, ProbeConcurrency: MaxProbeConcurrency, ProbeTimeoutSeconds: maxProbeTimeoutSeconds})
			if err != nil {
				t.Fatalf("expected max concurrency with max per-probe timeout to be valid, got %v", err)
			}
			if parallelBudget.ProbeConcurrency != MaxProbeConcurrency || parallelBudget.ProbeTimeoutSeconds != maxProbeTimeoutSeconds {
				t.Fatalf("parallel probe budget policy did not persist: %+v", parallelBudget)
			}
			_, err = store.UpsertWatchdogPolicy(ctx, SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, QuotaExceededPriority: 0, UsingPriority: 5, ProbeConcurrency: 1, ProbeTimeoutSeconds: maxProbeTimeoutSeconds + 1})
			if !IsStoreError(err, StoreErrorInvalidInput) {
				t.Fatalf("expected invalid per-probe timeout policy error, got %v", err)
			}
		})
	}
}

func TestWatchdogProbeObservationStoreRoundTrip(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "probe_observation_roundtrip") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "probe_observation_"+testCase.name)
			probedAt := sidecarStoreFixedNow().Add(-time.Minute)
			input := testProbeObservationInput(sidecar.ID, "auth-probe-primary", probedAt)
			created, err := store.CreateWatchdogProbeObservation(ctx, input)
			if err != nil {
				t.Fatalf("create probe observation: %v", err)
			}
			if created.ID == 0 || created.AuthID != "auth-probe-primary" || !created.QuotaExceeded || created.UpstreamStatusCode == nil || *created.UpstreamStatusCode != 200 {
				t.Fatalf("probe observation fields not persisted: %+v", created)
			}
			requireJSONEquivalent(t, created.WindowsJSON, `[{
				"source":"wham","window_type":"primary","used_percent":95.5,
				"limit_reached":true,"allowed":false,
				"reset_at":"2026-05-10T11:00:00Z","limit_window_seconds":3600
			}]`)
			listed, err := store.ListWatchdogProbeObservations(ctx, sidecar.ID, 10)
			if err != nil {
				t.Fatalf("list probe observations: %v", err)
			}
			if len(listed) != 1 || listed[0].ID != created.ID {
				t.Fatalf("expected created probe observation in list, got %+v", listed)
			}
		})
	}
}

func TestWatchdogProbeObservationRetention(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "probe_observation_retention") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "probe_retention_"+testCase.name)
			now := sidecarStoreFixedNow()
			old := testProbeObservationInput(sidecar.ID, "auth-old", now.Add(-15*24*time.Hour-time.Second))
			boundary := testProbeObservationInput(sidecar.ID, "auth-boundary", now.Add(-15*24*time.Hour))
			fresh := testProbeObservationInput(sidecar.ID, "auth-fresh", now.Add(-14*24*time.Hour))
			for _, input := range []SidecarWatchdogProbeObservationInput{old, boundary, fresh} {
				if _, err := store.CreateWatchdogProbeObservation(ctx, input); err != nil {
					t.Fatalf("seed probe observation %s: %v", input.AuthID, err)
				}
			}
			if _, err := store.CreateWatchdogAction(ctx, SidecarWatchdogActionInput{SidecarID: sidecar.ID, ActionType: "probe_retention_marker", Status: "succeeded"}); err != nil {
				t.Fatalf("seed action history marker: %v", err)
			}
			deleted, err := store.CleanupWatchdogProbeObservations(ctx)
			if err != nil {
				t.Fatalf("cleanup probe observations: %v", err)
			}
			if deleted != 1 {
				t.Fatalf("expected one old observation deleted, got %d", deleted)
			}
			remaining, err := store.ListWatchdogProbeObservations(ctx, sidecar.ID, 10)
			if err != nil {
				t.Fatalf("list retained probe observations: %v", err)
			}
			if len(remaining) != 2 || remaining[0].AuthID != "auth-fresh" || remaining[1].AuthID != "auth-boundary" {
				t.Fatalf("unexpected retained observations: %+v", remaining)
			}
			actions, err := store.ListWatchdogActions(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("list action history after cleanup: %v", err)
			}
			if len(actions) != 1 {
				t.Fatalf("probe cleanup must not touch action history, got %+v", actions)
			}
		})
	}
}

func TestApplyPendingWatchdogPolicyRevision(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "watchdog_policy_revision_apply")
	sidecar := createTestSidecar(t, ctx, store, "watchdog_policy_revision")
	active, err := store.CreateWatchdogPolicyRevision(ctx, SidecarWatchdogPolicyRevisionInput{SidecarID: sidecar.ID, Enabled: true, ProbeConcurrency: 2, ProbeTimeoutSeconds: 3})
	if err != nil {
		t.Fatalf("create active revision: %v", err)
	}
	if _, err := store.CreatePendingWatchdogPolicyRevision(ctx, SidecarWatchdogPolicyRevisionInput{SidecarID: sidecar.ID, Enabled: true, ProbeConcurrency: 5, ProbeTimeoutSeconds: 6}); err != nil {
		t.Fatalf("create pending revision: %v", err)
	}
	state, err := store.ApplyPendingWatchdogPolicyRevision(ctx, sidecar.ID)
	if err != nil {
		t.Fatalf("apply pending revision: %v", err)
	}
	if state.Policy.ActiveRevisionID == nil || *state.Policy.ActiveRevisionID == active.ID || state.Policy.PendingRevisionID != nil || state.ActiveRevision == nil || state.ActiveRevision.ProbeConcurrency != 5 || state.HasPendingChanges {
		t.Fatalf("pending revision was not applied: %+v", state)
	}
}

func TestResumePausedWatchdogSweep(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "watchdog_sweep_resume")
	sidecar := createTestSidecar(t, ctx, store, "watchdog_sweep_resume")
	revision, err := store.CreateWatchdogPolicyRevision(ctx, SidecarWatchdogPolicyRevisionInput{SidecarID: sidecar.ID, Enabled: true, ProbeConcurrency: 2, ProbeTimeoutSeconds: 3})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	paused, err := store.UpsertWatchdogSweep(ctx, SidecarWatchdogSweepInput{SweepID: "sweep-resume", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusPaused), SnapshotJSON: json.RawMessage(`[]`), NextItemIndex: 3, BatchIndex: 1})
	if err != nil {
		t.Fatalf("create paused sweep: %v", err)
	}
	result, err := store.ResumeWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: paused.SweepID, NextItemIndex: 4, BatchIndex: 2})
	if err != nil {
		t.Fatalf("resume sweep: %v", err)
	}
	if result.Outcome != SidecarWatchdogSweepMutationOutcomeUpdated || result.Sweep.Status != string(SidecarWatchdogSweepStatusRunning) || result.Sweep.NextItemIndex != 4 || result.Sweep.BatchIndex != 2 {
		t.Fatalf("unexpected resume result: %+v", result)
	}
}

func TestCompleteWatchdogSweep(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "watchdog_sweep_complete")
	sidecar := createTestSidecar(t, ctx, store, "watchdog_sweep_complete")
	revision, err := store.CreateWatchdogPolicyRevision(ctx, SidecarWatchdogPolicyRevisionInput{SidecarID: sidecar.ID, Enabled: true, ProbeConcurrency: 2, ProbeTimeoutSeconds: 3})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	running, err := store.UpsertWatchdogSweep(ctx, SidecarWatchdogSweepInput{SweepID: "sweep-complete", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: json.RawMessage(`[]`)})
	if err != nil {
		t.Fatalf("create running sweep: %v", err)
	}
	result, err := store.CompleteWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: running.SweepID, NextItemIndex: 10, BatchIndex: 3})
	if err != nil {
		t.Fatalf("complete sweep: %v", err)
	}
	if result.Outcome != SidecarWatchdogSweepMutationOutcomeUpdated || result.Sweep.Status != string(SidecarWatchdogSweepStatusCompleted) || result.Sweep.CompletedAt == nil || result.Sweep.LeaseExpiresAt != nil {
		t.Fatalf("unexpected completion result: %+v", result)
	}
}

func TestWatchdogSweepItemRoundTrip(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "watchdog_sweep_item_roundtrip")
	sidecar := createTestSidecar(t, ctx, store, "watchdog_sweep_item_roundtrip")
	revision, err := store.CreateWatchdogPolicyRevision(ctx, SidecarWatchdogPolicyRevisionInput{SidecarID: sidecar.ID, Enabled: true, ProbeConcurrency: 2, ProbeTimeoutSeconds: 3})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	now := sidecarStoreFixedNow()
	restartReason := "apply_and_restart"
	sweep, err := store.UpsertWatchdogSweep(ctx, SidecarWatchdogSweepInput{
		SweepID: "sweep-items-roundtrip", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID,
		Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: json.RawMessage(`[]`),
		RestartRequestedAt: &now, RestartTargetPolicyRevisionID: &revision.ID, RestartReason: &restartReason,
	})
	if err != nil {
		t.Fatalf("create sweep with durable restart intent: %v", err)
	}
	if sweep.RestartRequestedAt == nil || sweep.RestartTargetPolicyRevisionID == nil || *sweep.RestartTargetPolicyRevisionID != revision.ID || stringValue(sweep.RestartReason) != restartReason {
		t.Fatalf("restart intent did not persist on parent sweep: %+v", sweep)
	}
	leaseOwner := "worker-a"
	leaseExpiresAt := now.Add(time.Minute)
	completedAt := now.Add(2 * time.Minute)
	items, err := store.CreateWatchdogSweepItems(ctx, sweep.SweepID, []SidecarWatchdogSweepItemInput{
		{SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, ItemIndex: 0, Source: watchdogSweepSourceManualScanProbe, SourceRank: 10, Priority: 90, DueAt: &now, AuthID: "auth-a", SelectionJSON: json.RawMessage(`{"frozen":true}`)},
		{SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, ItemIndex: 1, Source: watchdogSweepSourceRollingRefreshProbe, SourceRank: 30, Priority: 50, AuthID: "auth-b", Status: string(SidecarWatchdogSweepItemStatusLeased), LeaseOwner: &leaseOwner, LeaseExpiresAt: &leaseExpiresAt, AttemptToken: 1},
		{SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, ItemIndex: 2, Source: watchdogSweepSourceDueHoldProbe, SourceRank: 0, Priority: 99, AuthID: "auth-c", Status: string(SidecarWatchdogSweepItemStatusSucceeded), CompletedAt: &completedAt},
	})
	if err != nil {
		t.Fatalf("create sweep items: %v", err)
	}
	if len(items) != 3 || items[0].Status != string(SidecarWatchdogSweepItemStatusQueued) || items[1].AttemptToken != 1 || items[2].CompletedAt == nil {
		t.Fatalf("unexpected created sweep items: %+v", items)
	}
	listed, err := store.ListWatchdogSweepItems(ctx, sweep.SweepID)
	if err != nil {
		t.Fatalf("list sweep items: %v", err)
	}
	if len(listed) != 3 || listed[0].AuthID != "auth-a" || listed[1].AuthID != "auth-b" || listed[2].AuthID != "auth-c" {
		t.Fatalf("sweep items did not round-trip in item_index order: %+v", listed)
	}
	requireJSONEqual(t, listed[0].SelectionJSON, `{"frozen":true}`)
	_, err = store.CreateWatchdogSweepItems(ctx, sweep.SweepID, []SidecarWatchdogSweepItemInput{{SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, ItemIndex: 0, Source: watchdogSweepSourceManualScanProbe, SourceRank: 10, AuthID: "auth-duplicate"}})
	if err == nil {
		t.Fatal("expected duplicate sweep item index to fail")
	}
	_, err = store.CreateWatchdogSweepItems(ctx, sweep.SweepID, []SidecarWatchdogSweepItemInput{{SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, ItemIndex: 3, Source: watchdogSweepSourceManualScanProbe, SourceRank: 10, AuthID: "auth-bad-lease", Status: string(SidecarWatchdogSweepItemStatusLeased)}})
	if !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected leased item without token metadata to fail validation, got %v", err)
	}
}

func TestWatchdogSweepIllegalTransitionsAreRejected(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "watchdog_sweep_illegal_transitions")
	_, _, sweep, _ := createWatchdogStoreSweepForTest(t, ctx, store, "illegal_transitions", SidecarWatchdogSweepStatusRunning, nil)

	resume, err := store.ResumeWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID})
	if err != nil || resume.Outcome != SidecarWatchdogSweepMutationOutcomeNotFound {
		t.Fatalf("running sweep must reject resume: result=%+v err=%v", resume, err)
	}
	paused, err := store.PauseWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID, NextItemIndex: 1, BatchIndex: 1})
	if err != nil || paused.Outcome != SidecarWatchdogSweepMutationOutcomeUpdated || paused.Sweep.Status != string(SidecarWatchdogSweepStatusPaused) {
		t.Fatalf("pause running sweep: result=%+v err=%v", paused, err)
	}

	for _, attempt := range []struct {
		name   string
		mutate func() (SidecarWatchdogSweepMutationResult, error)
	}{
		{name: "complete paused", mutate: func() (SidecarWatchdogSweepMutationResult, error) {
			return store.CompleteWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID})
		}},
		{name: "fail paused", mutate: func() (SidecarWatchdogSweepMutationResult, error) {
			return store.FailWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID})
		}},
		{name: "cancel paused", mutate: func() (SidecarWatchdogSweepMutationResult, error) {
			return store.CancelWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID})
		}},
	} {
		result, err := attempt.mutate()
		if err != nil || result.Outcome != SidecarWatchdogSweepMutationOutcomeNotFound {
			t.Fatalf("%s should be rejected without mutation: result=%+v err=%v", attempt.name, result, err)
		}
	}
	loaded, ok, err := store.GetWatchdogSweep(ctx, sweep.SweepID)
	if err != nil || !ok || loaded.Status != string(SidecarWatchdogSweepStatusPaused) || loaded.CompletedAt != nil {
		t.Fatalf("illegal paused transitions mutated sweep: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}

	resumed, err := store.ResumeWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID, NextItemIndex: loaded.NextItemIndex, BatchIndex: loaded.BatchIndex})
	if err != nil || resumed.Outcome != SidecarWatchdogSweepMutationOutcomeUpdated {
		t.Fatalf("resume paused sweep: result=%+v err=%v", resumed, err)
	}
	completedAt := sidecarStoreFixedNow().Add(time.Minute)
	completed, err := store.CompleteWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID, NextItemIndex: 1, BatchIndex: 1, CompletedAt: &completedAt})
	if err != nil || completed.Outcome != SidecarWatchdogSweepMutationOutcomeUpdated || completed.Sweep.Status != string(SidecarWatchdogSweepStatusCompleted) {
		t.Fatalf("complete resumed sweep: result=%+v err=%v", completed, err)
	}
	for _, attempt := range []struct {
		name   string
		mutate func() (SidecarWatchdogSweepMutationResult, error)
	}{
		{name: "resume completed", mutate: func() (SidecarWatchdogSweepMutationResult, error) {
			return store.ResumeWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID})
		}},
		{name: "complete completed", mutate: func() (SidecarWatchdogSweepMutationResult, error) {
			return store.CompleteWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID})
		}},
		{name: "fail completed", mutate: func() (SidecarWatchdogSweepMutationResult, error) {
			return store.FailWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID})
		}},
		{name: "cancel completed", mutate: func() (SidecarWatchdogSweepMutationResult, error) {
			return store.CancelWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: sweep.SweepID})
		}},
	} {
		result, err := attempt.mutate()
		if err != nil || result.Outcome != SidecarWatchdogSweepMutationOutcomeNotFound {
			t.Fatalf("%s should be rejected after terminal completion: result=%+v err=%v", attempt.name, result, err)
		}
	}
}

func TestWatchdogSweepItemLeaseClaimIsSingleOwnerAndTokenAdvances(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "watchdog_sweep_item_single_claim")
	_, _, sweep, _ := createWatchdogStoreSweepForTest(t, ctx, store, "single_claim", SidecarWatchdogSweepStatusRunning, []SidecarWatchdogSweepItemInput{{AuthID: "auth-single-claim"}})
	now := sidecarStoreFixedNow()

	first, err := store.ClaimWatchdogSweepItems(ctx, SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, Limit: 1, LeaseOwner: "worker-a", LeaseExpiresAt: now.Add(time.Minute), ClaimedAt: now})
	if err != nil || len(first) != 1 || first[0].AttemptToken != 1 || stringValue(first[0].LeaseOwner) != "worker-a" {
		t.Fatalf("first child lease claim failed: claimed=%+v err=%v", first, err)
	}
	blocked, err := store.ClaimWatchdogSweepItems(ctx, SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, Limit: 1, LeaseOwner: "worker-b", LeaseExpiresAt: now.Add(2 * time.Minute), ClaimedAt: now.Add(time.Second)})
	if err != nil || len(blocked) != 0 {
		t.Fatalf("active child lease should block a second owner: claimed=%+v err=%v", blocked, err)
	}
	reclaimedAt := now.Add(2 * time.Minute)
	reclaimed, err := store.ClaimWatchdogSweepItems(ctx, SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, Limit: 1, LeaseOwner: "worker-b", LeaseExpiresAt: reclaimedAt.Add(time.Minute), ClaimedAt: reclaimedAt})
	if err != nil || len(reclaimed) != 1 || reclaimed[0].ID != first[0].ID || reclaimed[0].AttemptToken != 2 || stringValue(reclaimed[0].LeaseOwner) != "worker-b" {
		t.Fatalf("expired child lease should advance token for new owner: claimed=%+v first=%+v err=%v", reclaimed, first, err)
	}
}

func TestWatchdogSweepItemCommitRejectsStaleLeaseTokenAfterReclaim(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "watchdog_sweep_item_stale_token")
	_, _, sweep, _ := createWatchdogStoreSweepForTest(t, ctx, store, "stale_token", SidecarWatchdogSweepStatusRunning, []SidecarWatchdogSweepItemInput{{AuthID: "auth-stale-token"}})
	now := sidecarStoreFixedNow()
	first, err := store.ClaimWatchdogSweepItems(ctx, SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, Limit: 1, LeaseOwner: "worker-a", LeaseExpiresAt: now.Add(time.Minute), ClaimedAt: now})
	if err != nil || len(first) != 1 {
		t.Fatalf("claim original lease: claimed=%+v err=%v", first, err)
	}
	reclaimedAt := now.Add(2 * time.Minute)
	reclaimed, err := store.ClaimWatchdogSweepItems(ctx, SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, Limit: 1, LeaseOwner: "worker-b", LeaseExpiresAt: reclaimedAt.Add(time.Minute), ClaimedAt: reclaimedAt})
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("reclaim expired lease: claimed=%+v err=%v", reclaimed, err)
	}

	staleCommit := SidecarWatchdogSweepItemCommitInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, ItemID: first[0].ID, ItemIndex: first[0].ItemIndex, AttemptToken: first[0].AttemptToken, LeaseOwner: "worker-a", Status: string(SidecarWatchdogSweepItemStatusSucceeded), CompletedAt: reclaimedAt.Add(10 * time.Second)}
	staleResult, err := store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: sweep.SidecarID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sweep.SidecarID, "auth-stale-token", reclaimedAt)}, SweepItemCommit: &staleCommit})
	if err != nil || staleResult.SweepItemCommit == nil || staleResult.SweepItemCommit.Outcome != SidecarWatchdogSweepItemCommitOutcomeStale || len(staleResult.Observations) != 0 || len(staleResult.QuotaStates) != 0 {
		t.Fatalf("stale lease-token commit should be fenced before side effects: result=%+v err=%v", staleResult, err)
	}
	listed, err := store.ListWatchdogSweepItems(ctx, sweep.SweepID)
	if err != nil || len(listed) != 1 || listed[0].Status != string(SidecarWatchdogSweepItemStatusLeased) || listed[0].AttemptToken != reclaimed[0].AttemptToken || stringValue(listed[0].LeaseOwner) != "worker-b" || listed[0].ResultObservationID != nil {
		t.Fatalf("stale commit mutated reclaimed lease: items=%+v err=%v", listed, err)
	}

	currentCommit := SidecarWatchdogSweepItemCommitInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, ItemID: reclaimed[0].ID, ItemIndex: reclaimed[0].ItemIndex, AttemptToken: reclaimed[0].AttemptToken, LeaseOwner: "worker-b", Status: string(SidecarWatchdogSweepItemStatusSucceeded), CompletedAt: reclaimedAt.Add(20 * time.Second)}
	currentResult, err := store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: sweep.SidecarID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sweep.SidecarID, "auth-stale-token", reclaimedAt.Add(20*time.Second))}, SweepItemCommit: &currentCommit})
	if err != nil || currentResult.SweepItemCommit == nil || currentResult.SweepItemCommit.Outcome != SidecarWatchdogSweepItemCommitOutcomeCommitted || len(currentResult.Observations) != 1 {
		t.Fatalf("current lease-token commit should succeed: result=%+v err=%v", currentResult, err)
	}
}

func TestWatchdogSweepItemDuplicateCommitIsIdempotent(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "watchdog_sweep_item_duplicate_commit")
	_, _, sweep, _ := createWatchdogStoreSweepForTest(t, ctx, store, "duplicate_commit", SidecarWatchdogSweepStatusRunning, []SidecarWatchdogSweepItemInput{{AuthID: "auth-duplicate-commit"}})
	now := sidecarStoreFixedNow()
	claimed, err := store.ClaimWatchdogSweepItems(ctx, SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, Limit: 1, LeaseOwner: "worker-a", LeaseExpiresAt: now.Add(time.Minute), ClaimedAt: now})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim duplicate commit item: claimed=%+v err=%v", claimed, err)
	}
	commit := SidecarWatchdogSweepItemCommitInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, ItemID: claimed[0].ID, ItemIndex: claimed[0].ItemIndex, AttemptToken: claimed[0].AttemptToken, LeaseOwner: "worker-a", Status: string(SidecarWatchdogSweepItemStatusSucceeded), CompletedAt: now.Add(time.Second)}
	first, err := store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: sweep.SidecarID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sweep.SidecarID, "auth-duplicate-commit", now.Add(time.Second))}, SweepItemCommit: &commit})
	if err != nil || first.SweepItemCommit == nil || first.SweepItemCommit.Outcome != SidecarWatchdogSweepItemCommitOutcomeCommitted || len(first.Observations) != 1 {
		t.Fatalf("first commit failed: result=%+v err=%v", first, err)
	}
	duplicate, err := store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: sweep.SidecarID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sweep.SidecarID, "auth-duplicate-replay", now.Add(2*time.Second))}, SweepItemCommit: &commit})
	if err != nil || duplicate.SweepItemCommit == nil || duplicate.SweepItemCommit.Outcome != SidecarWatchdogSweepItemCommitOutcomeDuplicate || len(duplicate.Observations) != 0 || len(duplicate.QuotaStates) != 0 {
		t.Fatalf("duplicate commit should be idempotent before side effects: result=%+v err=%v", duplicate, err)
	}
	observations, err := store.ListWatchdogProbeObservations(ctx, sweep.SidecarID, 10)
	if err != nil || len(observations) != 1 || observations[0].AuthID != "auth-duplicate-commit" {
		t.Fatalf("duplicate commit wrote extra observations: observations=%+v err=%v", observations, err)
	}
}

func TestWatchdogSweepCursorAdvancesAcrossContiguousTerminalItems(t *testing.T) {
	ctx, store, _ := sidecarStoreForTest(t, "watchdog_sweep_cursor_contiguous")
	now := sidecarStoreFixedNow()
	_, _, sweep, _ := createWatchdogStoreSweepForTest(t, ctx, store, "cursor_contiguous", SidecarWatchdogSweepStatusRunning, []SidecarWatchdogSweepItemInput{
		{ItemIndex: 0, AuthID: "auth-cursor-0", Status: string(SidecarWatchdogSweepItemStatusSucceeded), CompletedAt: &now},
		{ItemIndex: 1, AuthID: "auth-cursor-1"},
		{ItemIndex: 2, AuthID: "auth-cursor-2", Status: string(SidecarWatchdogSweepItemStatusFailed), CompletedAt: &now},
		{ItemIndex: 3, AuthID: "auth-cursor-3"},
	})
	claimed, err := store.ClaimWatchdogSweepItems(ctx, SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, StartItemIndex: 1, Limit: 1, LeaseOwner: "worker-cursor", LeaseExpiresAt: now.Add(time.Minute), ClaimedAt: now})
	if err != nil || len(claimed) != 1 || claimed[0].ItemIndex != 1 {
		t.Fatalf("claim cursor gap item: claimed=%+v err=%v", claimed, err)
	}
	commit := SidecarWatchdogSweepItemCommitInput{SweepID: sweep.SweepID, SidecarID: sweep.SidecarID, ItemID: claimed[0].ID, ItemIndex: claimed[0].ItemIndex, AttemptToken: claimed[0].AttemptToken, LeaseOwner: "worker-cursor", Status: string(SidecarWatchdogSweepItemStatusSucceeded), CompletedAt: now.Add(time.Second)}
	result, err := store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: sweep.SidecarID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sweep.SidecarID, "auth-cursor-1", now.Add(time.Second))}, SweepItemCommit: &commit})
	if err != nil || result.SweepItemCommit == nil || result.SweepItemCommit.Outcome != SidecarWatchdogSweepItemCommitOutcomeCommitted || result.SweepItemCommit.Sweep == nil || result.SweepItemCommit.Sweep.NextItemIndex != 3 {
		t.Fatalf("cursor did not advance across contiguous terminal prefix: result=%+v err=%v", result, err)
	}
	loaded, ok, err := store.GetWatchdogSweep(ctx, sweep.SweepID)
	if err != nil || !ok || loaded.NextItemIndex != 3 {
		t.Fatalf("persisted cursor mismatch after contiguous advancement: sweep=%+v ok=%v err=%v", loaded, ok, err)
	}
	items, err := store.ListWatchdogSweepItems(ctx, sweep.SweepID)
	if err != nil || len(items) != 4 || items[3].Status != string(SidecarWatchdogSweepItemStatusQueued) {
		t.Fatalf("cursor advancement should not terminalize non-contiguous tail: items=%+v err=%v", items, err)
	}
}

func TestWatchdogRuntimeMigrationCancelsLegacyActiveRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := sidecarPostgresHarness(t)
	databaseName := "watchdog_runtime_migration_" + sidecarRandomSuffix(t)
	conn := harness.openDatabase(t, ctx, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	beforeRunner, err := migrate.New(migrate.Options{MigrationsDir: sidecarMigrationsBeforeForStoreTest(t, "000022_sidecar_watchdog_sweep_items")})
	if err != nil {
		t.Fatalf("build pre-cleanup migration runner: %v", err)
	}
	if _, err := beforeRunner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations before cleanup: %v", err)
	}
	var sidecarID int
	if err := conn.QueryRow(ctx, `INSERT INTO sidecar_instances (name, base_url, base_url_canonical, management_password) VALUES ('Legacy runtime sidecar', 'https://legacy-runtime.example.test', 'https://legacy-runtime.example.test', 'enc:fixture') RETURNING id`).Scan(&sidecarID); err != nil {
		t.Fatalf("insert legacy sidecar: %v", err)
	}
	var policyID int
	if err := conn.QueryRow(ctx, `INSERT INTO sidecar_watchdog_policies (sidecar_id) VALUES ($1) RETURNING id`, sidecarID).Scan(&policyID); err != nil {
		t.Fatalf("insert legacy policy: %v", err)
	}
	var revisionID int64
	if err := conn.QueryRow(ctx, `INSERT INTO sidecar_watchdog_policy_revisions (policy_id, sidecar_id) VALUES ($1, $2) RETURNING id`, policyID, sidecarID).Scan(&revisionID); err != nil {
		t.Fatalf("insert legacy revision: %v", err)
	}
	if _, err := conn.Exec(ctx, `UPDATE sidecar_watchdog_policies SET active_revision_id=$2 WHERE id=$1`, policyID, revisionID); err != nil {
		t.Fatalf("activate legacy revision: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO sidecar_watchdog_sweeps (sweep_id, sidecar_id, policy_revision_id, status, snapshot_json, lease_expires_at) VALUES ('legacy-active-sweep', $1, $2, 'running', '[]'::jsonb, now() + interval '5 minutes')`, sidecarID, revisionID); err != nil {
		t.Fatalf("insert legacy active sweep: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO sidecar_quota_scan_runs (sidecar_id, scan_type, status) VALUES ($1, 'manual', 'queued')`, sidecarID); err != nil {
		t.Fatalf("insert legacy active quota scan: %v", err)
	}

	fullRunner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build full migration runner: %v", err)
	}
	result, err := fullRunner.Run(ctx, conn)
	if err != nil {
		t.Fatalf("run cleanup migration: %v", err)
	}
	if !stringSliceContainsForStoreTest(result.Versions, "000022_sidecar_watchdog_sweep_items") {
		t.Fatalf("cleanup migration was not applied: %+v", result)
	}
	var sweepStatus string
	var sweepCompleted, sweepLeaseCleared, sweepCancelRequested bool
	var sweepFailureReason, sweepCancelReason string
	if err := conn.QueryRow(ctx, `SELECT status, completed_at IS NOT NULL, lease_expires_at IS NULL, cancel_requested_at IS NOT NULL, COALESCE(failure_reason,''), COALESCE(cancel_reason,'') FROM sidecar_watchdog_sweeps WHERE sweep_id='legacy-active-sweep'`).Scan(&sweepStatus, &sweepCompleted, &sweepLeaseCleared, &sweepCancelRequested, &sweepFailureReason, &sweepCancelReason); err != nil {
		t.Fatalf("load migrated legacy sweep: %v", err)
	}
	if sweepStatus != string(SidecarWatchdogSweepStatusCancelled) || !sweepCompleted || !sweepLeaseCleared || !sweepCancelRequested || sweepFailureReason != "legacy_runtime_discarded" || sweepCancelReason != "legacy_runtime_discarded" {
		t.Fatalf("legacy active sweep was not deterministically cancelled: status=%s completed=%v leaseCleared=%v cancelRequested=%v failure=%q cancel=%q", sweepStatus, sweepCompleted, sweepLeaseCleared, sweepCancelRequested, sweepFailureReason, sweepCancelReason)
	}
	var scanStatus, scanError string
	var scanCompleted bool
	if err := conn.QueryRow(ctx, `SELECT status, completed_at IS NOT NULL, COALESCE(last_error_code,'') FROM sidecar_quota_scan_runs WHERE sidecar_id=$1`, sidecarID).Scan(&scanStatus, &scanCompleted, &scanError); err != nil {
		t.Fatalf("load migrated legacy quota scan: %v", err)
	}
	if scanStatus != quotaScanStatusCancelled || !scanCompleted || scanError != "legacy_runtime_discarded" {
		t.Fatalf("legacy active quota scan was not demoted to terminal projection: status=%s completed=%v error=%q", scanStatus, scanCompleted, scanError)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO sidecar_quota_scan_runs (sidecar_id, scan_type, status) VALUES ($1, 'manual', 'running')`, sidecarID); err == nil {
		t.Fatal("cleanup migration must reject new executable quota scan rows")
	}
}

func TestWatchdogRuntimeSchemaMigrationCleanup(t *testing.T) {
	ctx, store, pool := sidecarStoreForTest(t, "watchdog_runtime_schema_cleanup")
	sidecar := createTestSidecar(t, ctx, store, "watchdog_runtime_schema_cleanup")
	revision, err := store.CreateWatchdogPolicyRevision(ctx, SidecarWatchdogPolicyRevisionInput{SidecarID: sidecar.ID, Enabled: true, ProbeConcurrency: 2, ProbeTimeoutSeconds: 3})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	if _, err := store.UpsertWatchdogSweep(ctx, SidecarWatchdogSweepInput{SweepID: "sweep-active-parent", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: json.RawMessage(`[]`)}); err != nil {
		t.Fatalf("create active parent sweep: %v", err)
	}
	_, err = store.UpsertWatchdogSweep(ctx, SidecarWatchdogSweepInput{SweepID: "sweep-second-active-parent", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusPaused), SnapshotJSON: json.RawMessage(`[]`)})
	if !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected active parent uniqueness to reject second active sweep, got %v", err)
	}
	_, err = store.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeManual, Status: quotaScanStatusRunning, PlannedCount: 1})
	if !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected executable quota scan run creation to be rejected, got %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sidecar_quota_scan_runs (sidecar_id, scan_type, status) VALUES ($1, 'manual', 'running')`, sidecar.ID); err == nil {
		t.Fatal("expected schema to reject active quota scan run status")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sidecar_quota_scan_runs (sidecar_id, scan_type, status) VALUES ($1, 'manual', 'completed'), ($1, 'scheduled', 'failed')`, sidecar.ID); err != nil {
		t.Fatalf("projection-only quota scan history should remain insertable: %v", err)
	}
	var activeScanIndexExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname='public' AND indexname='uq_sidecar_quota_scan_runs_active_sidecar')`).Scan(&activeScanIndexExists); err != nil {
		t.Fatalf("check legacy active scan index absence: %v", err)
	}
	if activeScanIndexExists {
		t.Fatal("legacy active quota scan index should be removed")
	}
}

func TestWatchdogPrunesOlderTerminalSweeps(t *testing.T) {
	ctx, store, pool := sidecarStoreForTest(t, "watchdog_sweep_prunes_old_terminal")
	sidecar := createTestSidecar(t, ctx, store, "watchdog_sweep_prunes_old_terminal")
	revision, err := store.CreateWatchdogPolicyRevision(ctx, SidecarWatchdogPolicyRevisionInput{SidecarID: sidecar.ID, Enabled: true, ProbeConcurrency: 2, ProbeTimeoutSeconds: 3})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	now := sidecarStoreFixedNow()
	insertWatchdogSweepRowForPruning(t, ctx, pool, "sweep-old-completed", sidecar.ID, revision.ID, string(SidecarWatchdogSweepStatusCompleted), now.Add(-3*time.Hour), now.Add(-2*time.Hour))
	insertWatchdogSweepRowForPruning(t, ctx, pool, "sweep-old-failed", sidecar.ID, revision.ID, string(SidecarWatchdogSweepStatusFailed), now.Add(-2*time.Hour), now.Add(-time.Hour))
	running, err := store.UpsertWatchdogSweep(ctx, SidecarWatchdogSweepInput{SweepID: "sweep-new-completed", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: json.RawMessage(`[]`), StartedAt: now.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("create running sweep: %v", err)
	}
	if _, err := store.CompleteWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: running.SweepID, CompletedAt: &now}); err != nil {
		t.Fatalf("complete latest sweep: %v", err)
	}
	if _, ok, err := store.GetWatchdogSweep(ctx, "sweep-old-completed"); err != nil || ok {
		t.Fatalf("old completed sweep retained: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetWatchdogSweep(ctx, "sweep-old-failed"); err != nil || ok {
		t.Fatalf("old failed sweep retained: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetWatchdogSweep(ctx, "sweep-new-completed"); err != nil || !ok {
		t.Fatalf("latest completed sweep missing: ok=%v err=%v", ok, err)
	}
	terminalCount := countWatchdogSweepsForPruning(t, ctx, pool, sidecar.ID, true)
	if terminalCount != 1 {
		t.Fatalf("terminal sweep count = %d, want 1", terminalCount)
	}
}

func TestWatchdogLatestTerminalSweepRetained(t *testing.T) {
	ctx, store, pool := sidecarStoreForTest(t, "watchdog_sweep_retains_latest_terminal")
	sidecar := createTestSidecar(t, ctx, store, "watchdog_sweep_retains_latest_terminal")
	revision, err := store.CreateWatchdogPolicyRevision(ctx, SidecarWatchdogPolicyRevisionInput{SidecarID: sidecar.ID, Enabled: true, ProbeConcurrency: 2, ProbeTimeoutSeconds: 3})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	now := sidecarStoreFixedNow()
	insertWatchdogSweepRowForPruning(t, ctx, pool, "sweep-existing-latest", sidecar.ID, revision.ID, string(SidecarWatchdogSweepStatusCompleted), now.Add(-time.Hour), now)
	running, err := store.UpsertWatchdogSweep(ctx, SidecarWatchdogSweepInput{SweepID: "sweep-completed-older", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: json.RawMessage(`[]`), StartedAt: now.Add(-2 * time.Hour)})
	if err != nil {
		t.Fatalf("create running sweep: %v", err)
	}
	olderCompletedAt := now.Add(-30 * time.Minute)
	if _, err := store.CompleteWatchdogSweep(ctx, SidecarWatchdogSweepCheckpointInput{SweepID: running.SweepID, CompletedAt: &olderCompletedAt}); err != nil {
		t.Fatalf("complete older sweep: %v", err)
	}
	if _, ok, err := store.GetWatchdogSweep(ctx, "sweep-existing-latest"); err != nil || !ok {
		t.Fatalf("latest terminal sweep missing: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetWatchdogSweep(ctx, "sweep-completed-older"); err != nil || ok {
		t.Fatalf("older terminal sweep retained: ok=%v err=%v", ok, err)
	}
	latest, ok, err := store.GetLatestCompletedWatchdogSweep(ctx, sidecar.ID)
	if err != nil || !ok || latest.SweepID != "sweep-existing-latest" {
		t.Fatalf("latest completed sweep = %+v ok=%v err=%v", latest, ok, err)
	}
}

func insertWatchdogSweepRowForPruning(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sweepID string, sidecarID int, revisionID int64, status string, startedAt time.Time, completedAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO sidecar_watchdog_sweeps (sweep_id, sidecar_id, policy_revision_id, status, snapshot_json, started_at, completed_at) VALUES ($1,$2,$3,$4,'[]'::jsonb,$5,$6)`, sweepID, sidecarID, revisionID, status, startedAt, completedAt); err != nil {
		t.Fatalf("insert watchdog sweep %s: %v", sweepID, err)
	}
}

func countWatchdogSweepsForPruning(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sidecarID int, terminalOnly bool) int {
	t.Helper()
	query := `SELECT count(*) FROM sidecar_watchdog_sweeps WHERE sidecar_id=$1`
	if terminalOnly {
		query += ` AND status IN ('completed','failed','cancelled')`
	}
	var count int
	if err := pool.QueryRow(ctx, query, sidecarID).Scan(&count); err != nil {
		t.Fatalf("count watchdog sweeps: %v", err)
	}
	return count
}

func createWatchdogStoreSweepForTest(t *testing.T, ctx context.Context, store *Store, suffix string, status SidecarWatchdogSweepStatus, itemInputs []SidecarWatchdogSweepItemInput) (SidecarInstance, SidecarWatchdogPolicyRevision, SidecarWatchdogSweep, []SidecarWatchdogSweepItem) {
	t.Helper()
	sidecar := createTestSidecar(t, ctx, store, "watchdog_"+suffix)
	revision, err := store.CreateWatchdogPolicyRevision(ctx, SidecarWatchdogPolicyRevisionInput{SidecarID: sidecar.ID, Enabled: true, ProbeConcurrency: 2, ProbeTimeoutSeconds: 3})
	if err != nil {
		t.Fatalf("create watchdog policy revision: %v", err)
	}
	sweep, err := store.UpsertWatchdogSweep(ctx, SidecarWatchdogSweepInput{SweepID: "sweep-" + suffix, SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(status), SnapshotJSON: json.RawMessage(`[]`)})
	if err != nil {
		t.Fatalf("create watchdog sweep: %v", err)
	}
	if len(itemInputs) == 0 {
		return sidecar, revision, sweep, nil
	}
	inputs := make([]SidecarWatchdogSweepItemInput, 0, len(itemInputs))
	for index, input := range itemInputs {
		if strings.TrimSpace(input.SweepID) == "" {
			input.SweepID = sweep.SweepID
		}
		if input.SidecarID == 0 {
			input.SidecarID = sidecar.ID
		}
		if input.PolicyRevisionID == 0 {
			input.PolicyRevisionID = revision.ID
		}
		if input.ItemIndex == 0 && index > 0 {
			input.ItemIndex = index
		}
		if strings.TrimSpace(input.Source) == "" {
			input.Source = watchdogSweepSourceRollingRefreshProbe
		}
		if input.SourceRank == 0 {
			input.SourceRank = watchdogSweepSourceRank(input.Source)
		}
		if strings.TrimSpace(input.AuthID) == "" {
			input.AuthID = fmt.Sprintf("auth-%s-%d", suffix, index)
		}
		inputs = append(inputs, input)
	}
	items, err := store.CreateWatchdogSweepItems(ctx, sweep.SweepID, inputs)
	if err != nil {
		t.Fatalf("create watchdog sweep items: %v", err)
	}
	return sidecar, revision, sweep, items
}

func sidecarMigrationsBeforeForStoreTest(t *testing.T, excludedVersion string) string {
	t.Helper()
	sourceDir := migrate.DefaultMigrationsDir()
	targetDir := t.TempDir()
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		version := strings.TrimSuffix(entry.Name(), ".sql")
		if version >= excludedVersion {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(targetDir, entry.Name()), raw, 0o600); err != nil {
			t.Fatalf("copy migration %s: %v", entry.Name(), err)
		}
	}
	return targetDir
}

func stringSliceContainsForStoreTest(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestWatchdogDueHoldStoreOrdering(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "due_hold_ordering") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "due_hold_"+testCase.name)
			dueAt := sidecarStoreFixedNow()
			later, err := store.CreateWatchdogHold(ctx, testWatchdogHoldInput(sidecar.ID, "auth-later", dueAt.Add(-time.Hour)))
			if err != nil {
				t.Fatalf("create later due hold: %v", err)
			}
			earliest, err := store.CreateWatchdogHold(ctx, testWatchdogHoldInput(sidecar.ID, "auth-earliest", dueAt.Add(-2*time.Hour)))
			if err != nil {
				t.Fatalf("create earliest due hold: %v", err)
			}
			tie, err := store.CreateWatchdogHold(ctx, testWatchdogHoldInput(sidecar.ID, "auth-tie", dueAt.Add(-2*time.Hour)))
			if err != nil {
				t.Fatalf("create tied due hold: %v", err)
			}
			futureInput := testWatchdogHoldInput(sidecar.ID, "auth-future", dueAt.Add(time.Hour))
			if _, err := store.CreateWatchdogHold(ctx, futureInput); err != nil {
				t.Fatalf("create future hold: %v", err)
			}
			due, err := store.ListDueWatchdogHolds(ctx, sidecar.ID, dueAt)
			if err != nil {
				t.Fatalf("list due holds: %v", err)
			}
			if len(due) != 3 || due[0].ID != earliest.ID || due[1].ID != tie.ID || due[2].ID != later.ID {
				t.Fatalf("due holds not ordered by hold_until ASC, id ASC: %+v", due)
			}
		})
	}
}

func TestWatchdogProbeDecisionStoreAtomicity(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "probe_decision_atomicity") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "probe_atomic_"+testCase.name)
			decision := SidecarWatchdogProbeDecision{
				SidecarID: sidecar.ID,
				Observations: []SidecarWatchdogProbeObservationInput{
					testProbeObservationInput(sidecar.ID, "auth-atomic", sidecarStoreFixedNow()),
				},
				CreateHold: ptrWatchdogHoldInput(testWatchdogHoldInput(sidecar.ID, "auth-atomic", sidecarStoreFixedNow().Add(time.Hour))),
			}
			result, err := store.PersistWatchdogProbeDecision(ctx, decision)
			if err != nil {
				t.Fatalf("persist probe decision: %v", err)
			}
			if len(result.Observations) != 1 || result.CreatedHold == nil || result.Policy == nil || result.Policy.ProbeLastBatchCompletedAt == nil {
				t.Fatalf("probe decision result missing atomic state: %+v", result)
			}
			badObservation := testProbeObservationInput(sidecar.ID, "auth-bad", sidecarStoreFixedNow().Add(time.Minute))
			badObservation.WindowsJSON = json.RawMessage(`[{"raw_body":"secret-payload"}]`)
			_, err = store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{
				SidecarID:    sidecar.ID,
				Observations: []SidecarWatchdogProbeObservationInput{badObservation},
				CreateHold:   ptrWatchdogHoldInput(testWatchdogHoldInput(sidecar.ID, "auth-bad", sidecarStoreFixedNow().Add(2*time.Hour))),
			})
			if !IsStoreError(err, StoreErrorInvalidInput) {
				t.Fatalf("expected invalid atomic decision error, got %v", err)
			}
			observations, err := store.ListWatchdogProbeObservations(ctx, sidecar.ID, 10)
			if err != nil {
				t.Fatalf("list observations after failed decision: %v", err)
			}
			if len(observations) != 1 || observations[0].AuthID != "auth-atomic" {
				t.Fatalf("failed decision mutated observations: %+v", observations)
			}
			if _, ok, err := store.GetActiveWatchdogHold(ctx, sidecar.ID, "auth-bad"); err != nil || ok {
				t.Fatalf("failed decision mutated holds: ok=%v err=%v", ok, err)
			}
			policy, err := store.GetOrCreateWatchdogPolicy(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("reload policy after failed decision: %v", err)
			}
			if policy.ProbeLastBatchCompletedAt == nil {
				t.Fatalf("failed decision should preserve prior batch completion state: %+v", policy)
			}
		})
	}
}

func TestWatchdogQuotaStateProbeDecisionPersistence(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "quota_state_probe_decision") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "quota_state_probe_"+testCase.name)
			decision := SidecarWatchdogProbeDecision{SidecarID: sidecar.ID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sidecar.ID, "auth-quota", sidecarStoreFixedNow())}}
			result, err := store.PersistWatchdogProbeDecision(ctx, decision)
			if err != nil {
				t.Fatalf("persist probe decision: %v", err)
			}
			if len(result.QuotaStates) != 1 || result.QuotaStates[0].QuotaBand != "quota_exceeded" || result.QuotaStates[0].LastObservationID == nil || *result.QuotaStates[0].LastObservationID != result.Observations[0].ID {
				t.Fatalf("probe decision should materialize quota state from observation, got %+v", result.QuotaStates)
			}
			type authQuotaStateReader interface {
				ListAuthQuotaStates(context.Context, int) ([]SidecarAuthQuotaState, error)
			}
			reader, ok := store.(authQuotaStateReader)
			if !ok {
				t.Fatalf("store does not expose auth quota states")
			}
			states, err := reader.ListAuthQuotaStates(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("list quota states: %v", err)
			}
			if len(states) != 1 || states[0].AuthID != "auth-quota" || states[0].QuotaBand != "quota_exceeded" {
				t.Fatalf("quota state not persisted from probe decision, states=%+v", states)
			}
		})
	}
}

func TestWatchdogQuotaStateProbeDecisionPreservesSnapshotMetadata(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "quota_state_probe_metadata") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "quota_state_probe_metadata_"+testCase.name)
			snapshotObservedAt := sidecarStoreFixedNow().Add(-time.Hour)
			_, err := store.PersistQuotaProbeDecision(ctx, SidecarQuotaPersistDecision{
				SidecarID: sidecar.ID,
				QuotaStates: []SidecarAuthQuotaStateInput{{
					AuthID: "auth-quota-metadata", AuthIndex: stringPtr("snapshot_index"),
					AuthName: stringPtr("metadata.json"), Provider: stringPtr("gemini"),
					SnapshotObservedAt: &snapshotObservedAt, QuotaBand: quotaBandError, ReasonCode: stringPtr("unknown"),
				}},
			})
			if err != nil {
				t.Fatalf("seed snapshot quota state: %v", err)
			}
			observation := testProbeObservationInput(sidecar.ID, "auth-quota-metadata", sidecarStoreFixedNow())
			observation.AuthIndex = nil
			observation.Provider = nil
			result, err := store.PersistQuotaProbeDecision(ctx, SidecarQuotaPersistDecision{
				SidecarID:    sidecar.ID,
				Observations: []SidecarWatchdogProbeObservationInput{observation},
			})
			if err != nil {
				t.Fatalf("persist metadata probe observation: %v", err)
			}
			probeFieldsRefreshed := len(result.Observations) == 1 &&
				len(result.QuotaStates) == 1 &&
				result.QuotaStates[0].LastObservationID != nil &&
				*result.QuotaStates[0].LastObservationID == result.Observations[0].ID
			if !probeFieldsRefreshed {
				t.Fatalf("probe observation did not refresh quota probe fields: %+v", result.QuotaStates)
			}
			states := listQuotaStatesForProbeStore(t, ctx, store, sidecar.ID)
			if len(states) != 1 {
				t.Fatalf("expected one quota state, got %+v", states)
			}
			state := states[0]
			metadataPreserved := stringValue(state.AuthIndex) == "snapshot_index" &&
				stringValue(state.AuthName) == "metadata.json" &&
				stringValue(state.Provider) == "gemini" &&
				state.SnapshotObservedAt != nil && state.SnapshotObservedAt.Equal(snapshotObservedAt)
			if !metadataPreserved {
				t.Fatalf("probe-derived update blanked snapshot metadata: %+v", state)
			}
			stateRefreshed := state.QuotaBand == "quota_exceeded" &&
				state.ProbeStatus != nil && *state.ProbeStatus == watchdogProbeStatusSucceeded
			if !stateRefreshed {
				t.Fatalf("probe-derived update did not refresh latest observation state: %+v", state)
			}
		})
	}
}

func TestWatchdogQuotaPersistObservationAdvancesCooldownWithPersistedAttempt(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "quota_persist_cooldown") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "quota_persist_cooldown_"+testCase.name)

			result, err := store.PersistQuotaProbeDecision(ctx, SidecarQuotaPersistDecision{
				SidecarID:    sidecar.ID,
				Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sidecar.ID, "auth-cooldown", sidecarStoreFixedNow())},
			})
			if err != nil {
				t.Fatalf("persist quota probe decision: %v", err)
			}
			if len(result.Observations) != 1 || result.Policy == nil || result.Policy.ProbeLastBatchCompletedAt == nil {
				t.Fatalf("persisted probe attempt did not advance cooldown: %+v", result)
			}
			policy, err := store.GetOrCreateWatchdogPolicy(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("reload policy after quota persist: %v", err)
			}
			if policy.ProbeLastBatchCompletedAt == nil {
				t.Fatalf("cooldown timestamp was not persisted: %+v", policy)
			}
		})
	}
}

func TestWatchdogQuotaPersistCooldownGateIgnoresZeroAttemptStateOnlyBatch(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "quota_persist_zero_attempt") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "quota_persist_zero_"+testCase.name)
			scanRun, err := store.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeManual, Status: quotaScanStatusCompleted, PlannedCount: 1})
			if err != nil {
				t.Fatalf("create quota scan run: %v", err)
			}
			scanProgressMarker := "auth-after-zero"

			result, err := store.PersistQuotaProbeDecision(ctx, SidecarQuotaPersistDecision{
				SidecarID: sidecar.ID,
				QuotaStates: []SidecarAuthQuotaStateInput{{
					AuthID:     "auth-unsupported",
					AuthIndex:  stringPtr("auth_unsupported"),
					Provider:   stringPtr("gemini"),
					QuotaBand:  quotaBandError,
					ReasonCode: stringPtr("unsupported_provider"),
				}},
				ScanRunID:    &scanRun.ID,
				CursorAuthID: &scanProgressMarker,
			})
			if err != nil {
				t.Fatalf("persist state-only quota decision: %v", err)
			}
			if len(result.Observations) != 0 || result.Policy != nil || result.ScanRun != nil || len(result.QuotaStates) != 1 {
				t.Fatalf("zero-attempt persist mutated attempted-only state: %+v", result)
			}
			states := listQuotaStatesForProbeStore(t, ctx, store, sidecar.ID)
			if len(states) != 1 || states[0].AuthID != "auth-unsupported" || states[0].QuotaBand != quotaBandError || states[0].ReasonCode == nil || *states[0].ReasonCode != "unsupported_provider" {
				t.Fatalf("state-only quota persist did not write latest state: %+v", states)
			}
			policy, err := store.GetOrCreateWatchdogPolicy(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("reload policy after state-only persist: %v", err)
			}
			if policy.ProbeLastBatchCompletedAt != nil {
				t.Fatalf("zero-attempt persist advanced cooldown: %+v", policy)
			}
			reloadedRun, ok, err := store.GetQuotaScanRun(ctx, sidecar.ID, scanRun.ID)
			if err != nil || !ok || reloadedRun.AttemptedCount != 0 || reloadedRun.CursorAuthID != nil {
				t.Fatalf("zero-attempt persist advanced scan progress: run=%+v ok=%v err=%v", reloadedRun, ok, err)
			}
		})
	}
}

func TestWatchdogQuotaPersistAtomicRollbackCoversAllState(t *testing.T) {
	now := sidecarStoreFixedNow()
	for _, testCase := range watchdogProbeStoreCases(t, "quota_persist_atomic_rollback") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "quota_persist_rollback_"+testCase.name)
			scanRun, err := store.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeInitial, Status: quotaScanStatusCompleted, PlannedCount: 2})
			if err != nil {
				t.Fatalf("create quota scan run: %v", err)
			}
			if _, err := store.CreateWatchdogHold(ctx, testWatchdogHoldInput(sidecar.ID, "auth-conflict", now.Add(time.Hour))); err != nil {
				t.Fatalf("seed active hold: %v", err)
			}
			cursor := "auth-after-conflict"
			conflictingHold := testWatchdogHoldInput(sidecar.ID, "auth-conflict", now.Add(2*time.Hour))

			_, err = store.PersistQuotaProbeDecision(ctx, SidecarQuotaPersistDecision{
				SidecarID:    sidecar.ID,
				Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sidecar.ID, "auth-conflict", now)},
				QuotaStates: []SidecarAuthQuotaStateInput{{
					AuthID:     "auth-state-only",
					AuthIndex:  stringPtr("auth_state_only"),
					Provider:   stringPtr("gemini"),
					QuotaBand:  quotaBandError,
					ReasonCode: stringPtr("unsupported_provider"),
				}},
				CreateHold:   &conflictingHold,
				ScanRunID:    &scanRun.ID,
				CursorAuthID: &cursor,
			})
			if !IsStoreError(err, StoreErrorDuplicateActiveHold) {
				t.Fatalf("expected duplicate hold rollback error, got %v", err)
			}
			observations, err := store.ListWatchdogProbeObservations(ctx, sidecar.ID, 10)
			if err != nil || len(observations) != 0 {
				t.Fatalf("failed quota persist mutated observations: observations=%+v err=%v", observations, err)
			}
			for _, state := range listQuotaStatesForProbeStore(t, ctx, store, sidecar.ID) {
				if state.AuthID == "auth-conflict" || state.AuthID == "auth-state-only" {
					t.Fatalf("failed quota persist mutated quota state: %+v", state)
				}
			}
			reloadedRun, ok, err := store.GetQuotaScanRun(ctx, sidecar.ID, scanRun.ID)
			if err != nil || !ok || reloadedRun.AttemptedCount != 0 || reloadedRun.CursorAuthID != nil {
				t.Fatalf("failed quota persist mutated scan run: run=%+v ok=%v err=%v", reloadedRun, ok, err)
			}
			policy, err := store.GetOrCreateWatchdogPolicy(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("reload policy after failed quota persist: %v", err)
			}
			if policy.ProbeLastBatchCompletedAt != nil {
				t.Fatalf("failed quota persist advanced cooldown: %+v", policy)
			}
			if _, ok, err := store.GetActiveWatchdogHold(ctx, sidecar.ID, "auth-conflict"); err != nil || !ok {
				t.Fatalf("failed quota persist removed existing hold: ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestWatchdogActionHistoryAndPendingQueueRemainDecoupled(t *testing.T) {
	type pendingActionStore interface {
		watchdogProbeStoreForTest
		GetWatchdogActionByHistoryKey(context.Context, int, time.Time, int) (SidecarWatchdogAction, bool, error)
		CreateWatchdogPendingAction(context.Context, SidecarWatchdogPendingActionInput) (SidecarWatchdogPendingAction, error)
		ListWatchdogPendingActions(context.Context, int) ([]SidecarWatchdogPendingAction, error)
		ClaimWatchdogPendingActions(context.Context, int, int) ([]SidecarWatchdogPendingAction, error)
		DeleteWatchdogPendingAction(context.Context, int, int) (bool, error)
	}
	for _, testCase := range watchdogProbeStoreCases(t, "watchdog_queue_history_decoupled") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			queueStore, ok := store.(pendingActionStore)
			if !ok {
				t.Fatalf("store does not support pending watchdog actions")
			}
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "queue_history_"+testCase.name)
			authID := "auth-queue-history"
			authName := "queue-history.json"
			action, err := queueStore.CreateWatchdogAction(ctx, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: &authID, AuthName: &authName, ActionType: watchdogActionDeprioritize, Status: watchdogActionStatusPending, TargetPriority: intPtr(0)})
			if err != nil {
				t.Fatalf("create action history row: %v", err)
			}
			pending, err := queueStore.CreateWatchdogPendingAction(ctx, SidecarWatchdogPendingActionInput{SidecarID: sidecar.ID, ActionHistoryCreatedAt: action.CreatedAt, ActionHistoryID: action.ID, AuthID: &authID, AuthName: &authName, ActionType: action.ActionType, TargetPriority: intPtr(0)})
			if err != nil {
				t.Fatalf("create pending queue row: %v", err)
			}
			claimed, err := queueStore.ClaimWatchdogPendingActions(ctx, sidecar.ID, 1)
			if err != nil || len(claimed) != 1 || claimed[0].ID != pending.ID || claimed[0].AttemptCount != 1 {
				t.Fatalf("claim pending queue row: claimed=%+v err=%v", claimed, err)
			}
			loadedAction, found, err := queueStore.GetWatchdogActionByHistoryKey(ctx, sidecar.ID, action.CreatedAt, action.ID)
			if err != nil || !found || loadedAction.Status != watchdogActionStatusPending {
				t.Fatalf("queue claim mutated retained history: action=%+v found=%v err=%v", loadedAction, found, err)
			}
			deleted, err := queueStore.DeleteWatchdogPendingAction(ctx, sidecar.ID, pending.ID)
			if err != nil || !deleted {
				t.Fatalf("delete pending queue row: deleted=%v err=%v", deleted, err)
			}
			pendingRows, err := queueStore.ListWatchdogPendingActions(ctx, sidecar.ID)
			if err != nil || len(pendingRows) != 0 {
				t.Fatalf("pending queue delete did not clear queue: rows=%+v err=%v", pendingRows, err)
			}
			actions, err := queueStore.ListWatchdogActions(ctx, sidecar.ID)
			if err != nil || len(actions) != 1 || actions[0].ID != action.ID || actions[0].Status != watchdogActionStatusPending {
				t.Fatalf("pending queue delete mutated retained history: actions=%+v err=%v", actions, err)
			}
		})
	}
}

func TestMemorySidecarStoreModelParity(t *testing.T) {
	ctx := context.Background()
	store := newMemorySidecarStore(sidecarStoreFixedNow, sidecarStoreSecretKey)
	sidecar := createTestSidecarInProbeStore(t, ctx, store, "memory_model_parity")

	policy, err := store.UpsertWatchdogPolicy(ctx, SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, Enabled: true})
	if err != nil {
		t.Fatalf("upsert memory watchdog policy: %v", err)
	}
	if policy.ProbeBatchCooldownSeconds != DefaultProbeBatchCooldownSeconds || !policy.QuotaInventoryEnabled || !policy.InitialScanEnabled || !policy.RollingRefreshEnabled || policy.RollingRefreshAfterSeconds != DefaultRollingRefreshAfterSeconds {
		t.Fatalf("memory policy missing expanded defaults: %+v", policy)
	}

	action, err := store.CreateWatchdogAction(ctx, SidecarWatchdogActionInput{SidecarID: sidecar.ID, ActionType: watchdogActionDeprioritize, Status: watchdogActionStatusPending})
	if err != nil {
		t.Fatalf("create memory action history row: %v", err)
	}
	pending, err := store.CreateWatchdogPendingAction(ctx, SidecarWatchdogPendingActionInput{SidecarID: sidecar.ID, ActionHistoryCreatedAt: action.CreatedAt, ActionHistoryID: action.ID, ActionType: action.ActionType})
	if err != nil {
		t.Fatalf("create memory pending queue row: %v", err)
	}
	pendingRows, err := store.ListWatchdogPendingActions(ctx, sidecar.ID)
	if err != nil || len(pendingRows) != 1 || pendingRows[0].ID != pending.ID {
		t.Fatalf("pending queue row not listed independently: rows=%+v err=%v", pendingRows, err)
	}
	if deleted, err := store.DeleteWatchdogPendingAction(ctx, sidecar.ID, pending.ID); err != nil || !deleted {
		t.Fatalf("delete memory pending row: deleted=%v err=%v", deleted, err)
	}
	pendingRows, err = store.ListWatchdogPendingActions(ctx, sidecar.ID)
	if err != nil || len(pendingRows) != 0 {
		t.Fatalf("pending queue delete did not clear queue: rows=%+v err=%v", pendingRows, err)
	}
	actions, err := store.ListWatchdogActions(ctx, sidecar.ID)
	if err != nil || len(actions) != 1 || actions[0].ID != action.ID {
		t.Fatalf("pending queue delete mutated retained history: actions=%+v err=%v", actions, err)
	}

	scanRun, err := store.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: "initial", Status: "completed", PlannedCount: 2})
	if err != nil {
		t.Fatalf("create memory scan run: %v", err)
	}
	if _, err := store.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: "manual", Status: "queued"}); !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected active memory scan conflict, got %v", err)
	}
	completedAt := sidecarStoreFixedNow().Add(time.Minute)
	if _, err := store.UpdateQuotaScanRun(ctx, scanRun.ID, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: "initial", Status: "completed", PlannedCount: 2, CompletedAt: &completedAt}); err != nil {
		t.Fatalf("complete memory scan run: %v", err)
	}
	state, err := store.UpsertAuthQuotaState(ctx, SidecarAuthQuotaStateInput{SidecarID: sidecar.ID, AuthID: "auth-memory", AuthName: stringPtr("auth-memory.json"), QuotaBand: quotaBandError, ReasonCode: stringPtr("unknown")})
	if err != nil || state.QuotaBand != quotaBandError || state.ReasonCode == nil || *state.ReasonCode != "unknown" || stringValue(state.AuthName) != "auth-memory.json" {
		t.Fatalf("upsert memory quota state: state=%+v err=%v", state, err)
	}
}

func TestMemoryProbeDecisionPersistsQuotaStateAndScanAtomically(t *testing.T) {
	now := sidecarStoreFixedNow()
	ctx := context.Background()
	store := newMemorySidecarStore(func() time.Time { return now }, sidecarStoreSecretKey)
	sidecar := createTestSidecarInProbeStore(t, ctx, store, "memory_probe_atomic")
	scanRun, err := store.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: "initial", Status: "completed", PlannedCount: 2})
	if err != nil {
		t.Fatalf("create memory scan run: %v", err)
	}
	cursor := "auth-next"
	decision := SidecarWatchdogProbeDecision{SidecarID: sidecar.ID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sidecar.ID, "auth-memory-quota", now)}, AdvanceCursor: true, CursorAuthID: &cursor, ScanRunID: &scanRun.ID}

	result, err := store.PersistWatchdogProbeDecision(ctx, decision)
	if err != nil {
		t.Fatalf("persist memory probe decision: %v", err)
	}
	if len(result.Observations) != 1 || len(result.QuotaStates) != 1 || result.ScanRun != nil || result.Policy == nil || result.Policy.ProbeLastBatchCompletedAt == nil {
		t.Fatalf("memory probe decision missing merged state or mutated projection-only scan run: %+v", result)
	}
	if result.QuotaStates[0].QuotaBand != "quota_exceeded" || result.QuotaStates[0].LastObservationID == nil || *result.QuotaStates[0].LastObservationID != result.Observations[0].ID {
		t.Fatalf("memory quota state not derived from observation: %+v", result.QuotaStates[0])
	}
	reloadedProjection, ok, err := store.GetQuotaScanRun(ctx, sidecar.ID, scanRun.ID)
	if err != nil || !ok || reloadedProjection.AttemptedCount != 0 || reloadedProjection.CursorAuthID != nil {
		t.Fatalf("projection-only scan run was mutated: run=%+v ok=%v err=%v", reloadedProjection, ok, err)
	}

	badObservation := testProbeObservationInput(sidecar.ID, "auth-memory-bad", now.Add(time.Minute))
	badObservation.WindowsJSON = json.RawMessage(`[{"]raw_body":"secret-payload"}]`)
	_, err = store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: sidecar.ID, Observations: []SidecarWatchdogProbeObservationInput{badObservation}, ScanRunID: &scanRun.ID})
	if !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected invalid memory decision error, got %v", err)
	}
	observations, err := store.ListWatchdogProbeObservations(ctx, sidecar.ID, 10)
	if err != nil || len(observations) != 1 || observations[0].AuthID != "auth-memory-quota" {
		t.Fatalf("failed memory decision mutated observations: observations=%+v err=%v", observations, err)
	}
	states, err := store.ListAuthQuotaStates(ctx, sidecar.ID)
	if err != nil || len(states) != 1 || states[0].AuthID != "auth-memory-quota" {
		t.Fatalf("failed memory decision mutated quota states: states=%+v err=%v", states, err)
	}
	reloadedRun, ok, err := store.GetQuotaScanRun(ctx, sidecar.ID, scanRun.ID)
	if err != nil || !ok || reloadedRun.AttemptedCount != 0 || reloadedRun.QuotaExceededCount != 0 {
		t.Fatalf("failed memory decision mutated projection scan run: run=%+v ok=%v err=%v", reloadedRun, ok, err)
	}
}

func TestQuotaScanRunLifecycleAndReplaceParity(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "quota_scan_lifecycle") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "quota_scan_lifecycle_"+testCase.name)
			activeRun, err := store.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeInitial, Status: quotaScanStatusCompleted, PlannedCount: 2})
			if err != nil {
				t.Fatalf("create active quota scan run: %v", err)
			}
			if _, err := store.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeManual, Status: quotaScanStatusQueued, PlannedCount: 1}); !IsStoreError(err, StoreErrorInvalidInput) {
				t.Fatalf("expected active quota scan conflict, got %v", err)
			}
			cancelledAt := sidecarStoreFixedNow().Add(time.Minute)
			cancelled, err := store.UpdateQuotaScanRun(ctx, activeRun.ID, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeInitial, Status: quotaScanStatusCancelled, PlannedCount: 2, CancelRequestedAt: &cancelledAt, CompletedAt: &cancelledAt})
			if err != nil {
				t.Fatalf("cancel active quota scan run: %v", err)
			}
			if cancelled.Status != quotaScanStatusCancelled || cancelled.CancelRequestedAt == nil || cancelled.CompletedAt == nil {
				t.Fatalf("cancelled scan run missing persisted cancellation metadata: %+v", cancelled)
			}
			replacement, err := store.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeManual, Status: quotaScanStatusFailed, PlannedCount: 1})
			if err != nil {
				t.Fatalf("create replacement quota scan projection: %v", err)
			}
			if replacement.ID == activeRun.ID || replacement.Status != quotaScanStatusFailed {
				t.Fatalf("replacement scan projection did not persist correctly: active=%+v replacement=%+v", cancelled, replacement)
			}
			runs, err := store.ListQuotaScanRuns(ctx, sidecar.ID)
			if err != nil || len(runs) != 2 {
				t.Fatalf("list quota scan runs after replace: runs=%+v err=%v", runs, err)
			}
		})
	}
}

func TestQuotaScanRunDecisionPersistsProgressAndRollsBack(t *testing.T) {
	now := sidecarStoreFixedNow()
	for _, testCase := range watchdogProbeStoreCases(t, "quota_scan_progress") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "quota_scan_progress_"+testCase.name)
			scanRun, err := store.CreateQuotaScanRun(ctx, SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeInitial, Status: quotaScanStatusCompleted, PlannedCount: 2})
			if err != nil {
				t.Fatalf("create quota scan run: %v", err)
			}
			cursor := "auth-next"
			decision := SidecarWatchdogProbeDecision{SidecarID: sidecar.ID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sidecar.ID, "auth-memory-quota", now)}, AdvanceCursor: true, CursorAuthID: &cursor, ScanRunID: &scanRun.ID}
			result, err := store.PersistWatchdogProbeDecision(ctx, decision)
			if err != nil {
				t.Fatalf("persist quota scan decision: %v", err)
			}
			if len(result.Observations) != 1 || len(result.QuotaStates) != 1 || result.ScanRun != nil {
				t.Fatalf("quota scan decision mutated projection-only scan progress: %+v", result.ScanRun)
			}
			reloadedRun, ok, err := store.GetQuotaScanRun(ctx, sidecar.ID, scanRun.ID)
			if err != nil || !ok || reloadedRun.AttemptedCount != 0 || reloadedRun.UsingCount != 0 || reloadedRun.QuotaExceededCount != 0 || reloadedRun.ErrorCount != 0 || reloadedRun.CursorAuthID != nil {
				t.Fatalf("projection-only quota scan run was mutated: run=%+v ok=%v err=%v", reloadedRun, ok, err)
			}
			badObservation := testProbeObservationInput(sidecar.ID, "auth-memory-bad", now.Add(time.Minute))
			badObservation.WindowsJSON = json.RawMessage(`[{"raw_body":"secret-payload"}]`)
			_, err = store.PersistWatchdogProbeDecision(ctx, SidecarWatchdogProbeDecision{SidecarID: sidecar.ID, Observations: []SidecarWatchdogProbeObservationInput{badObservation}, ScanRunID: &scanRun.ID})
			if !IsStoreError(err, StoreErrorInvalidInput) {
				t.Fatalf("expected invalid quota scan decision error, got %v", err)
			}
			observations, err := store.ListWatchdogProbeObservations(ctx, sidecar.ID, 10)
			if err != nil || len(observations) != 1 || observations[0].AuthID != "auth-memory-quota" {
				t.Fatalf("failed quota scan decision mutated observations: observations=%+v err=%v", observations, err)
			}
			reloadedRun, ok, err = store.GetQuotaScanRun(ctx, sidecar.ID, scanRun.ID)
			if err != nil || !ok || reloadedRun.AttemptedCount != 0 || reloadedRun.QuotaExceededCount != 0 {
				t.Fatalf("failed quota scan decision mutated projection scan run: run=%+v ok=%v err=%v", reloadedRun, ok, err)
			}
		})
	}
}

func TestProbeObservationPrivacyRejectsSensitiveWindowFields(t *testing.T) {
	for _, testCase := range watchdogProbeStoreCases(t, "probe_privacy") {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.ctx, testCase.store
			sidecar := createTestSidecarInProbeStore(t, ctx, store, "probe_privacy_"+testCase.name)
			sensitiveWindows := []json.RawMessage{
				json.RawMessage(`[{"token":"raw-token"}]`),
				json.RawMessage(`[{"account_id":"acct_123"}]`),
				json.RawMessage(`[{"account":"acct_123"}]`),
				json.RawMessage(`[{"user":"raw-user"}]`),
				json.RawMessage(`[{"username":"raw-user"}]`),
				json.RawMessage(`[{"email":"operator@example.test"}]`),
				json.RawMessage(`[{"request_headers":"raw-headers"}]`),
				json.RawMessage(`[{"response_body":"raw-body"}]`),
				json.RawMessage(`[{"authorization_header":"Bearer token"}]`),
				json.RawMessage(`[{"headers":{"authorization":"Bearer token"}}]`),
			}
			for _, windows := range sensitiveWindows {
				input := testProbeObservationInput(sidecar.ID, "auth-sensitive", sidecarStoreFixedNow())
				input.WindowsJSON = windows
				_, err := store.CreateWatchdogProbeObservation(ctx, input)
				if !IsStoreError(err, StoreErrorInvalidInput) {
					t.Fatalf("expected sensitive windows_json to be rejected, got %v for %s", err, string(windows))
				}
			}
		})
	}
}

func TestObservationRawPayloadColumnsAbsent(t *testing.T) {
	ctx, _, pool := sidecarStoreForTest(t, "observation_raw_columns")
	rows, err := pool.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'sidecar_watchdog_probe_observations'`)
	if err != nil {
		t.Fatalf("query observation columns: %v", err)
	}
	defer rows.Close()
	bannedFragments := []string{"header", "body", "token", "account", "email", "cookie", "user_id", "useridentity", "raw"}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan observation column: %v", err)
		}
		normalized := strings.ReplaceAll(strings.ToLower(column), "-", "_")
		for _, banned := range bannedFragments {
			if strings.Contains(normalized, banned) {
				t.Fatalf("probe observations must not persist raw/private column %q", column)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate observation columns: %v", err)
	}
}

type watchdogProbeStoreForTest interface {
	CreateSidecarInstance(context.Context, SidecarInstanceInput) (SidecarInstance, error)
	GetOrCreateWatchdogPolicy(context.Context, int) (SidecarWatchdogPolicy, error)
	UpsertWatchdogPolicy(context.Context, SidecarWatchdogPolicyInput) (SidecarWatchdogPolicy, error)
	CreateWatchdogProbeObservation(context.Context, SidecarWatchdogProbeObservationInput) (SidecarWatchdogProbeObservation, error)
	ListWatchdogProbeObservations(context.Context, int, int) ([]SidecarWatchdogProbeObservation, error)
	CleanupWatchdogProbeObservations(context.Context) (int64, error)
	PersistWatchdogProbeDecision(context.Context, SidecarWatchdogProbeDecision) (SidecarWatchdogProbeDecisionResult, error)
	PersistQuotaProbeDecision(context.Context, SidecarQuotaPersistDecision) (SidecarQuotaPersistResult, error)
	CreateQuotaScanRun(context.Context, SidecarQuotaScanRunInput) (SidecarQuotaScanRun, error)
	UpdateQuotaScanRun(context.Context, int, SidecarQuotaScanRunInput) (SidecarQuotaScanRun, error)
	GetQuotaScanRun(context.Context, int, int) (SidecarQuotaScanRun, bool, error)
	ListQuotaScanRuns(context.Context, int) ([]SidecarQuotaScanRun, error)
	CreateWatchdogHold(context.Context, SidecarWatchdogHoldInput) (SidecarWatchdogHold, error)
	GetActiveWatchdogHold(context.Context, int, string) (SidecarWatchdogHold, bool, error)
	ListDueWatchdogHolds(context.Context, int, time.Time) ([]SidecarWatchdogHold, error)
	CreateWatchdogAction(context.Context, SidecarWatchdogActionInput) (SidecarWatchdogAction, error)
	ListWatchdogActions(context.Context, int) ([]SidecarWatchdogAction, error)
}

type watchdogProbeStoreCase struct {
	name  string
	ctx   context.Context
	store watchdogProbeStoreForTest
}

func watchdogProbeStoreCases(t *testing.T, name string) []watchdogProbeStoreCase {
	t.Helper()
	ctx, postgresStore, _ := sidecarStoreForTest(t, name+"_postgres")
	memoryStore := newMemorySidecarStore(sidecarStoreFixedNow, sidecarStoreSecretKey)
	return []watchdogProbeStoreCase{
		{name: "postgres", ctx: ctx, store: postgresStore},
		{name: "memory", ctx: ctx, store: memoryStore},
	}
}

func sidecarStoreFixedNow() time.Time {
	return time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
}

func createTestSidecarInProbeStore(t *testing.T, ctx context.Context, store watchdogProbeStoreForTest, suffix string) SidecarInstance {
	t.Helper()
	host := strings.ReplaceAll(suffix, "_", "-") + ".example.test"
	created, err := store.CreateSidecarInstance(ctx, SidecarInstanceInput{
		Name:               "Probe Sidecar " + suffix,
		BaseURL:            "https://" + host + "/",
		BaseURLCanonical:   "https://" + host,
		ManagementPassword: "password-" + suffix,
	})
	if err != nil {
		t.Fatalf("create probe test sidecar %s: %v", suffix, err)
	}
	return created
}

func testProbeObservationInput(sidecarID int, authID string, probedAt time.Time) SidecarWatchdogProbeObservationInput {
	status := 200
	resetAt := time.Date(2026, time.May, 10, 11, 0, 0, 0, time.UTC)
	return SidecarWatchdogProbeObservationInput{
		SidecarID:          sidecarID,
		AuthID:             authID,
		AuthIndex:          stringPtr("auth_001"),
		Provider:           stringPtr("codex"),
		ProbedAt:           probedAt,
		ProbeStatus:        "probe_succeeded",
		UpstreamStatusCode: &status,
		QuotaExceeded:      true,
		ReasonCode:         stringPtr("primary_window_exhausted"),
		QuotaResetAt:       &resetAt,
		BlockingWindow:     stringPtr("primary"),
		WindowsJSON:        json.RawMessage(`[{"source":"wham","window_type":"primary","used_percent":95.5,"limit_reached":true,"allowed":false,"reset_at":"2026-05-10T11:00:00Z","limit_window_seconds":3600}]`),
	}
}

func testWatchdogHoldInput(sidecarID int, authID string, holdUntil time.Time) SidecarWatchdogHoldInput {
	return SidecarWatchdogHoldInput{
		SidecarID:      sidecarID,
		AuthID:         authID,
		AuthIndex:      stringPtr("auth_001"),
		Provider:       stringPtr("codex"),
		Reason:         watchdogReasonQuotaExceeded,
		ConditionHash:  "condition-" + authID,
		TargetPriority: DefaultQuotaExceededPriority,
		HoldUntil:      &holdUntil,
		Status:         WatchdogHoldStatusActive,
	}
}

func ptrWatchdogHoldInput(input SidecarWatchdogHoldInput) *SidecarWatchdogHoldInput {
	return &input
}

func listQuotaStatesForProbeStore(t *testing.T, ctx context.Context, store watchdogProbeStoreForTest, sidecarID int) []SidecarAuthQuotaState {
	t.Helper()
	type authQuotaStateReader interface {
		ListAuthQuotaStates(context.Context, int) ([]SidecarAuthQuotaState, error)
	}
	reader, ok := store.(authQuotaStateReader)
	if !ok {
		t.Fatalf("store does not expose auth quota states")
	}
	states, err := reader.ListAuthQuotaStates(ctx, sidecarID)
	if err != nil {
		t.Fatalf("list quota states: %v", err)
	}
	return states
}
