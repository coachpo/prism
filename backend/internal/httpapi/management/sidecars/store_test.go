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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
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
	auth, err := store.SaveAuthFile(ctx, SidecarAuthFileInput{
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
		t.Fatalf("validate auth file: %v", err)
	}
	if auth.StorageKey != "auth-gemini-primary" || !auth.MutationSafe {
		t.Fatalf("expected mutable returned auth file, got %+v", auth)
	}
	loaded, ok, err := store.GetAuthFile(ctx, sidecar.ID, "auth-gemini-primary")
	if err != nil || ok || loaded.AuthID != "" {
		t.Fatalf("postgres auth files should not be retained: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	authList, err := store.ListAuthFiles(ctx, sidecar.ID)
	if err != nil {
		t.Fatalf("list auth files: %v", err)
	}
	if len(authList) != 0 {
		t.Fatalf("expected postgres auth file list to be live-only and empty, got %+v", authList)
	}
	_, err = store.SaveAuthFile(ctx, SidecarAuthFileInput{SidecarID: sidecar.ID, AuthID: "raw-secret", Name: "raw-secret.json", SnapshotJSON: json.RawMessage(`{"api-key":"raw-secret-value"}`)})
	if !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected raw auth file secret to be rejected, got %v", err)
	}
	_, err = store.SaveAuthFile(ctx, SidecarAuthFileInput{SidecarID: sidecar.ID, AuthID: "raw-password", Name: "raw-password.json", SnapshotJSON: json.RawMessage(`{"metadata":{"password":"raw-password-value","management_key":"raw-management-key"}}`)})
	if !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected nested raw password/management key auth file secret to be rejected, got %v", err)
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

func TestReplaceAuthFilesNormalizesGenerationWithoutPersisting(t *testing.T) {
	for _, testCase := range authFileReplacementStoreCases() {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.setup(t, "auth_replace_generation_"+testCase.name)
			sidecar := createTestSidecarInReplacementStore(t, ctx, store, "auth_replace_generation_"+testCase.name)
			if _, err := store.SaveAuthFile(ctx, testAuthFileInput(sidecar.ID, "auth-stale", "stale.json", 0)); err != nil {
				t.Fatalf("seed stale auth file: %v", err)
			}
			if _, err := store.SaveAuthFile(ctx, testAuthFileInput(sidecar.ID, "auth-keep", "keep.json", 0)); err != nil {
				t.Fatalf("seed kept auth file: %v", err)
			}

			replaced, err := store.ReplaceAuthFiles(ctx, sidecar.ID, []SidecarAuthFileInput{
				testAuthFileInput(sidecar.ID, "auth-keep", "keep.json", 1),
				testAuthFileInput(sidecar.ID, "auth-new", "new.json", 1),
			})
			if err != nil {
				t.Fatalf("normalize auth replacement rows: %v", err)
			}
			if len(replaced) != 2 {
				t.Fatalf("expected two normalized records, got %+v", replaced)
			}
			replacedByAuthID := requireAuthFileSet(t, replaced, "auth-keep", "auth-new")
			requireJSONEqual(t, replacedByAuthID["auth-keep"].SnapshotJSON, `{"generation":1}`)

			listed, err := store.ListAuthFiles(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("list auth files after replacement normalization: %v", err)
			}
			listedByAuthID := requireAuthFileSet(t, listed, "auth-keep", "auth-stale")
			requireJSONEqual(t, listedByAuthID["auth-keep"].SnapshotJSON, `{"generation":0}`)

			repeated, err := store.ReplaceAuthFiles(ctx, sidecar.ID, []SidecarAuthFileInput{
				testAuthFileInput(sidecar.ID, "auth-keep", "keep.json", 2),
			})
			if err != nil {
				t.Fatalf("repeat auth replacement normalization: %v", err)
			}
			repeatedByAuthID := requireAuthFileSet(t, repeated, "auth-keep")
			requireJSONEqual(t, repeatedByAuthID["auth-keep"].SnapshotJSON, `{"generation":2}`)

			listed, err = store.ListAuthFiles(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("list auth files after repeated replacement normalization: %v", err)
			}
			listedByAuthID = requireAuthFileSet(t, listed, "auth-keep", "auth-stale")
			requireJSONEqual(t, listedByAuthID["auth-keep"].SnapshotJSON, `{"generation":0}`)
		})
	}
}

func TestReplaceAuthFilesEmptyGenerationDoesNotClearRows(t *testing.T) {
	for _, testCase := range authFileReplacementStoreCases() {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.setup(t, "auth_replace_empty_"+testCase.name)
			sidecar := createTestSidecarInReplacementStore(t, ctx, store, "auth_replace_empty_"+testCase.name)
			if _, err := store.SaveAuthFile(ctx, testAuthFileInput(sidecar.ID, "auth-a", "a.json", 0)); err != nil {
				t.Fatalf("seed first auth file: %v", err)
			}
			if _, err := store.SaveAuthFile(ctx, testAuthFileInput(sidecar.ID, "auth-b", "b.json", 0)); err != nil {
				t.Fatalf("seed second auth file: %v", err)
			}

			replaced, err := store.ReplaceAuthFiles(ctx, sidecar.ID, nil)
			if err != nil {
				t.Fatalf("normalize empty auth generation: %v", err)
			}
			if len(replaced) != 0 {
				t.Fatalf("expected empty normalized result, got %+v", replaced)
			}
			listed, err := store.ListAuthFiles(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("list auth files after empty normalization: %v", err)
			}
			requireAuthFileSet(t, listed, "auth-a", "auth-b")
		})
	}
}

func TestReplaceAuthFilesValidationPreservesPreviousGeneration(t *testing.T) {
	for _, testCase := range authFileReplacementStoreCases() {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, store := testCase.setup(t, "auth_replace_validate_"+testCase.name)
			sidecar := createTestSidecarInReplacementStore(t, ctx, store, "auth_replace_validate_"+testCase.name)
			if _, err := store.SaveAuthFile(ctx, testAuthFileInput(sidecar.ID, "auth-old", "old.json", 0)); err != nil {
				t.Fatalf("seed previous auth file: %v", err)
			}

			_, err := store.ReplaceAuthFiles(ctx, sidecar.ID, []SidecarAuthFileInput{
				testAuthFileInput(sidecar.ID, "auth-new", "new.json", 1),
				{SidecarID: sidecar.ID, AuthID: "auth-invalid", Name: "invalid.json", SnapshotJSON: json.RawMessage(`{"api_key":"raw-secret-value"}`)},
			})
			if !IsStoreError(err, StoreErrorInvalidInput) {
				t.Fatalf("expected invalid replacement input error, got %v", err)
			}
			listed, err := store.ListAuthFiles(ctx, sidecar.ID)
			if err != nil {
				t.Fatalf("list auth files after invalid replacement: %v", err)
			}
			byAuthID := requireAuthFileSet(t, listed, "auth-old")
			requireJSONEqual(t, byAuthID["auth-old"].SnapshotJSON, `{"generation":0}`)
		})
	}
}

type authFileReplacementTestStore interface {
	CreateSidecarInstance(context.Context, SidecarInstanceInput) (SidecarInstance, error)
	SaveAuthFile(context.Context, SidecarAuthFileInput) (SidecarAuthFile, error)
	ReplaceAuthFiles(context.Context, int, []SidecarAuthFileInput) ([]SidecarAuthFile, error)
	ListAuthFiles(context.Context, int) ([]SidecarAuthFile, error)
}

type authFileReplacementStoreCase struct {
	name  string
	setup func(*testing.T, string) (context.Context, authFileReplacementTestStore)
}

func authFileReplacementStoreCases() []authFileReplacementStoreCase {
	return []authFileReplacementStoreCase{
		{
			name: "memory",
			setup: func(t *testing.T, _ string) (context.Context, authFileReplacementTestStore) {
				t.Helper()
				return context.Background(), newMemorySidecarStore(func() time.Time {
					return time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
				}, sidecarStoreSecretKey)
			},
		},
	}
}

func createTestSidecarInReplacementStore(t *testing.T, ctx context.Context, store authFileReplacementTestStore, suffix string) SidecarInstance {
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

func testAuthFileInput(sidecarID int, authID string, name string, generation int) SidecarAuthFileInput {
	return SidecarAuthFileInput{
		SidecarID:    sidecarID,
		AuthID:       authID,
		Name:         name,
		Status:       stringPtr(fmt.Sprintf("generation-%d", generation)),
		SnapshotJSON: json.RawMessage(fmt.Sprintf(`{"generation":%d}`, generation)),
	}
}

func requireAuthFileSet(t *testing.T, snapshots []SidecarAuthFile, wantAuthIDs ...string) map[string]SidecarAuthFile {
	t.Helper()
	if len(snapshots) != len(wantAuthIDs) {
		t.Fatalf("auth file count = %d, want %d: %+v", len(snapshots), len(wantAuthIDs), snapshots)
	}
	byAuthID := make(map[string]SidecarAuthFile, len(snapshots))
	for _, snapshot := range snapshots {
		if _, exists := byAuthID[snapshot.AuthID]; exists {
			t.Fatalf("duplicate auth file for auth_id %s in %+v", snapshot.AuthID, snapshots)
		}
		byAuthID[snapshot.AuthID] = snapshot
	}
	for _, authID := range wantAuthIDs {
		if _, exists := byAuthID[authID]; !exists {
			t.Fatalf("missing auth file %s in %+v", authID, snapshots)
		}
	}
	return byAuthID
}

func TestSidecarStoreDatabaseUsesCurrentSchema(t *testing.T) {
	ctx, _, pool := sidecarStoreForTest(t, "current_sidecar_schema")
	assertSidecarStoreCurrentTables(t, ctx, pool)
	assertSidecarStoreRetentionColumns(t, ctx, pool)
}

func assertSidecarStoreCurrentTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind IN ('r', 'p')
		  AND left(c.relname, length('sidecar_')) = 'sidecar_'
		ORDER BY c.relname ASC`)
	if err != nil {
		t.Fatalf("load current sidecar tables: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			t.Fatalf("scan current sidecar table: %v", err)
		}
		got[tableName] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate current sidecar tables: %v", err)
	}
	assertSidecarStoreStringSet(t, "sidecar tables", got, map[string]bool{"sidecar_instances": true, "sidecar_provider_snapshots": true})
}

func assertSidecarStoreRetentionColumns(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'log_retention_settings'
		  AND right(column_name, length('_retention_days')) = '_retention_days'
		ORDER BY column_name ASC`)
	if err != nil {
		t.Fatalf("load current log retention columns: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			t.Fatalf("scan current log retention column: %v", err)
		}
		got[columnName] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate current log retention columns: %v", err)
	}
	assertSidecarStoreStringSet(t, "log retention day columns", got, map[string]bool{"audit_logs_retention_days": true, "loadbalance_events_retention_days": true, "request_logs_retention_days": true, "statistics_retention_days": true})
}

func assertSidecarStoreStringSet(t *testing.T, label string, got map[string]bool, expected map[string]bool) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("%s = %v want %v", label, got, expected)
	}
	for value := range expected {
		if !got[value] {
			t.Fatalf("%s missing %s: got %v want %v", label, value, got, expected)
		}
	}
	for value := range got {
		if !expected[value] {
			t.Fatalf("%s has unexpected %s: got %v want %v", label, value, got, expected)
		}
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

func stringPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }

func intPtr(value int) *int { return &value }

func TestMemorySidecarStoreModelParity(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	store := newMemorySidecarStore(func() time.Time { return now }, sidecarStoreSecretKey)
	sidecar, err := store.CreateSidecarInstance(ctx, SidecarInstanceInput{
		Name:                  "Memory Core Sidecar",
		BaseURL:               "https://memory-core.example.test/",
		BaseURLCanonical:      "https://memory-core.example.test",
		ManagementPassword:    "memory-password",
		Enabled:               boolPtr(true),
		SyncIntervalSeconds:   60,
		RequestTimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("create memory sidecar: %v", err)
	}

	priority := 10
	disabled := false
	if _, err := store.SaveAuthFile(ctx, SidecarAuthFileInput{
		SidecarID:          sidecar.ID,
		AuthID:             "auth-memory",
		AuthIndex:          stringPtr("auth_memory"),
		Name:               "auth-memory.json",
		Provider:           stringPtr("gemini"),
		Status:             stringPtr("active"),
		Disabled:           &disabled,
		Priority:           &priority,
		RecentRequestsJSON: json.RawMessage(`[]`),
		ModelStatesJSON:    json.RawMessage(`{}`),
		SnapshotJSON:       json.RawMessage(`{"id":"auth-memory","priority":10}`),
		ObservedAt:         now,
	}); err != nil {
		t.Fatalf("save memory auth file: %v", err)
	}
	authFiles, err := store.ListAuthFiles(ctx, sidecar.ID)
	if err != nil {
		t.Fatalf("list memory auth files: %v", err)
	}
	byAuthID := requireAuthFileSet(t, authFiles, "auth-memory")
	if byAuthID["auth-memory"].Priority == nil || *byAuthID["auth-memory"].Priority != priority {
		t.Fatalf("memory auth file priority did not round-trip: %+v", byAuthID["auth-memory"])
	}

	if _, err := store.SaveProviderSnapshot(ctx, SidecarProviderSnapshotInput{
		SidecarID:       sidecar.ID,
		ProviderKey:     "gemini-api-key",
		ProviderItemKey: "auth_memory",
		Name:            stringPtr("Gemini memory"),
		Status:          stringPtr("configured"),
		Disabled:        &disabled,
		SnapshotJSON:    json.RawMessage(`{"auth-index":"auth_memory"}`),
		ObservedAt:      now,
	}); err != nil {
		t.Fatalf("save memory provider snapshot: %v", err)
	}
	providerSnapshots, err := store.ListProviderSnapshots(ctx, sidecar.ID)
	if err != nil || len(providerSnapshots) != 1 || providerSnapshots[0].ProviderKey != "gemini-api-key" {
		t.Fatalf("provider snapshot did not round-trip: snapshots=%+v err=%v", providerSnapshots, err)
	}

	syncAt := now.Add(time.Minute)
	staleAfter := now.Add(time.Hour)
	updated, err := store.UpdateSidecarSyncMetadata(ctx, SidecarSyncMetadataInput{
		SidecarID:            sidecar.ID,
		LastSyncAt:           syncAt,
		LastSuccessfulSyncAt: &syncAt,
		SnapshotStaleAfter:   &staleAfter,
		ManagementAuthState:  ManagementAuthStateValid,
	})
	if err != nil {
		t.Fatalf("update memory sync metadata: %v", err)
	}
	reloaded, found, err := store.GetSidecarInstance(ctx, sidecar.ID)
	if err != nil || !found || reloaded.LastSuccessfulSyncAt == nil || !reloaded.LastSuccessfulSyncAt.Equal(syncAt) || updated.ManagementAuthState != ManagementAuthStateValid {
		t.Fatalf("memory sidecar sync metadata did not round-trip: sidecar=%+v updated=%+v found=%v err=%v", reloaded, updated, found, err)
	}
}
