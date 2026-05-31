package configbundle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

func TestExportKeepsOneOwnerConnectionRefs(t *testing.T) {
	models := []modelRow{{ID: 1, ModelID: "gpt-4o-mini", DisplayName: stringPtr("GPT 4o Mini")}}
	accessTargetsByModelID := map[int][]accessTargetRow{
		1: {{SourceModelConfigID: 1, TargetType: "connection", TargetConnectionID: intPtr(10), Position: 0, IsEnabled: true}},
	}
	connectionRefByID := map[int]string{10: "openai-primary"}

	if err := validateExportedConnectionOwners(models, accessTargetsByModelID, connectionRefByID); err != nil {
		t.Fatalf("validate one-owner export: %v", err)
	}
	exportedTargets, err := buildAccessTargetExports(models[0], accessTargetsByModelID[1], connectionRefByID)
	if err != nil {
		t.Fatalf("build access target exports: %v", err)
	}
	if len(exportedTargets) != 1 || exportedTargets[0].ConnectionRef == nil || *exportedTargets[0].ConnectionRef != "openai-primary" {
		t.Fatalf("expected one exported connection_ref target, got %+v", exportedTargets)
	}
}

func TestExportRejectsDuplicateConnectionRefOwners(t *testing.T) {
	models := []modelRow{
		{ID: 1, ModelID: "gpt-4o-mini", DisplayName: stringPtr("GPT 4o Mini")},
		{ID: 2, ModelID: "gpt-4o-alt", DisplayName: stringPtr("GPT 4o Alt")},
	}
	accessTargetsByModelID := map[int][]accessTargetRow{
		1: {{SourceModelConfigID: 1, TargetType: "connection", TargetConnectionID: intPtr(10), Position: 0, IsEnabled: true}},
		2: {{SourceModelConfigID: 2, TargetType: "connection", TargetConnectionID: intPtr(10), Position: 0, IsEnabled: true}},
	}
	connectionRefByID := map[int]string{10: "openai-primary"}

	err := validateExportedConnectionOwners(models, accessTargetsByModelID, connectionRefByID)
	requireConfigBundleDomainError(t, err, 400, "connection_ref 'openai-primary' is owned by multiple models: model_id 'gpt-4o-mini' (display_name 'GPT 4o Mini') and model_id 'gpt-4o-alt' (display_name 'GPT 4o Alt')")
}

func TestBuildEndpointExportsSecretSafety(t *testing.T) {
	const profileSecretKey = "profile-secret-key"
	encryptedAPIKey, err := endpointdomain.EncryptSecret(" live-secret ", profileSecretKey, func() time.Time {
		return time.Unix(1, 0).UTC()
	})
	if err != nil {
		t.Fatalf("encrypt endpoint secret: %v", err)
	}

	encryptCalls := 0
	service := &Service{
		secretEncryptionKey: profileSecretKey,
		bundleSecretEncrypter: func(value string) (string, error) {
			encryptCalls++
			if value != "live-secret" {
				t.Fatalf("expected decrypted endpoint secret, got %q", value)
			}
			return "enc:bundle-secret", nil
		},
	}
	endpoints := []endpointRow{{ID: 1, Name: "Primary", BaseURL: "https://api.example.test", APIKey: encryptedAPIKey, Position: 0}}

	safeEndpoints, endpointByID, safeSecrets, err := service.buildEndpointExports(endpoints, false)
	if err != nil {
		t.Fatalf("build safe endpoint exports: %v", err)
	}
	if endpointByID[1].Name != "Primary" || len(safeEndpoints) != 1 || safeEndpoints[0].APIKeySecretRef != nil {
		t.Fatalf("expected safe endpoint export to preserve endpoint and omit secret ref, got endpoints=%+v byID=%+v", safeEndpoints, endpointByID)
	}
	if safeSecrets == nil || len(safeSecrets) != 0 || encryptCalls != 0 {
		t.Fatalf("expected safe export to avoid encrypted secret entries, got secrets=%+v encryptCalls=%d", safeSecrets, encryptCalls)
	}

	dangerousEndpoints, _, dangerousSecrets, err := service.buildEndpointExports(endpoints, true)
	if err != nil {
		t.Fatalf("build dangerous endpoint exports: %v", err)
	}
	if len(dangerousEndpoints) != 1 || dangerousEndpoints[0].APIKeySecretRef == nil || *dangerousEndpoints[0].APIKeySecretRef != "endpoint:Primary:api_key" {
		t.Fatalf("expected dangerous endpoint export to carry secret ref, got %+v", dangerousEndpoints)
	}
	if len(dangerousSecrets) != 1 || dangerousSecrets[0].Ref != "endpoint:Primary:api_key" || dangerousSecrets[0].Ciphertext != "enc:bundle-secret" || encryptCalls != 1 {
		t.Fatalf("expected dangerous export secret entry, got secrets=%+v encryptCalls=%d", dangerousSecrets, encryptCalls)
	}
}

func TestBuildVendorCatalogUsesCanonicalEnvelope(t *testing.T) {
	exportTime := time.Unix(1700000000, 0).UTC()
	exec := testQueryExecutor{rows: newTestRows(
		[]any{"openai", "OpenAI", sql.NullString{String: "Primary vendor", Valid: true}, sql.NullString{String: "openai", Valid: true}, true, true},
		[]any{"anthropic", "Anthropic", sql.NullString{}, sql.NullString{}, false, false},
	)}

	bundle, err := buildVendorCatalog(context.Background(), exec, exportTime)
	if err != nil {
		t.Fatalf("build vendor catalog: %v", err)
	}
	if bundle.Version != canonicalVendorCatalogVersion || bundle.BundleKind != canonicalVendorCatalogKind || !bundle.ExportedAt.Equal(exportTime) {
		t.Fatalf("expected canonical vendor catalog envelope, got version=%d kind=%q exportedAt=%s", bundle.Version, bundle.BundleKind, bundle.ExportedAt)
	}
	if len(bundle.Vendors) != 2 {
		t.Fatalf("expected two exported vendors, got %d", len(bundle.Vendors))
	}
	if bundle.Vendors[0].Description == nil || *bundle.Vendors[0].Description != "Primary vendor" {
		t.Fatalf("expected first vendor description to round-trip, got %+v", bundle.Vendors[0])
	}
	if bundle.Vendors[1].Description != nil || bundle.Vendors[1].IconKey != nil {
		t.Fatalf("expected nullable vendor fields to stay nil, got %+v", bundle.Vendors[1])
	}
}

func TestVendorCatalogImportNormalizesAndRejectsDuplicates(t *testing.T) {
	request := normalizeVendorCatalogImportRequest(vendorCatalogImportRequest{
		Version:    canonicalVendorCatalogVersion,
		BundleKind: canonicalVendorCatalogKind,
		Vendors: []vendorCatalogRow{
			{Key: " OpenAI-1 ", Name: " OpenAI ", Description: stringPtr(" Primary vendor "), IconKey: stringPtr(" CLOUD "), AuditEnabled: true, AuditCaptureBodies: true},
			{Key: " openai-1 ", Name: "OpenAI Mirror", Description: stringPtr(" Secondary vendor "), IconKey: stringPtr(" EDGE ")},
			{Key: " OpenAI-2 ", Name: "OpenAI", AuditCaptureBodies: true},
		},
	})
	if request.Vendors[0].Key != "openai-1" || request.Vendors[0].Name != "OpenAI" {
		t.Fatalf("expected first vendor to be normalized, got %+v", request.Vendors[0])
	}
	if request.Vendors[0].Description == nil || *request.Vendors[0].Description != "Primary vendor" {
		t.Fatalf("expected first vendor description to be trimmed, got %+v", request.Vendors[0])
	}
	if request.Vendors[0].IconKey == nil || *request.Vendors[0].IconKey != "cloud" {
		t.Fatalf("expected first vendor icon key to be normalized, got %+v", request.Vendors[0])
	}

	createCount, updateCount, unchangedCount, blockingErrors, _, err := countVendorCatalogChanges(context.Background(), testQueryExecutor{rows: newTestRows()}, request)
	if err != nil {
		t.Fatalf("count vendor catalog changes: %v", err)
	}
	if createCount != 3 || updateCount != 0 || unchangedCount != 0 {
		t.Fatalf("expected duplicate-only bundle to remain create-shaped, got create=%d update=%d unchanged=%d", createCount, updateCount, unchangedCount)
	}
	expectedErrors := []string{
		"Vendor catalog bundle contains duplicate vendor key 'openai-1'",
		"Vendor catalog bundle contains duplicate vendor name 'OpenAI' for keys 'openai-1' and 'openai-2'",
	}
	if !reflect.DeepEqual(blockingErrors, expectedErrors) {
		t.Fatalf("expected normalized duplicate errors %v, got %v", expectedErrors, blockingErrors)
	}
}

type testQueryExecutor struct {
	rows     pgx.Rows
	queryErr error
}

func (stub testQueryExecutor) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (stub testQueryExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if stub.queryErr != nil {
		return nil, stub.queryErr
	}
	if stub.rows != nil {
		return stub.rows, nil
	}
	return newTestRows(), nil
}

func (testQueryExecutor) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

type testRows struct {
	values  [][]any
	current int
	readErr error
}

func newTestRows(values ...[]any) *testRows {
	return &testRows{values: values, current: -1}
}

func (rows *testRows) Close() {}

func (rows *testRows) Err() error {
	return rows.readErr
}

func (rows *testRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (rows *testRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (rows *testRows) Next() bool {
	if rows.current+1 >= len(rows.values) {
		return false
	}
	rows.current++
	return true
}

func (rows *testRows) Scan(dest ...any) error {
	if rows.current < 0 || rows.current >= len(rows.values) {
		return errors.New("scan called without a current row")
	}
	current := rows.values[rows.current]
	if len(dest) != len(current) {
		return fmt.Errorf("scan destination mismatch: got %d want %d", len(dest), len(current))
	}
	for index := range dest {
		if err := assignTestRowValue(dest[index], current[index]); err != nil {
			return fmt.Errorf("scan column %d: %w", index, err)
		}
	}
	return nil
}

func (rows *testRows) Values() ([]any, error) {
	if rows.current < 0 || rows.current >= len(rows.values) {
		return nil, errors.New("values called without a current row")
	}
	return append([]any(nil), rows.values[rows.current]...), nil
}

func (rows *testRows) RawValues() [][]byte {
	return nil
}

func (rows *testRows) Conn() *pgx.Conn {
	return nil
}

func assignTestRowValue(destination any, value any) error {
	switch dest := destination.(type) {
	case *int:
		resolved, ok := value.(int)
		if !ok {
			return fmt.Errorf("expected int value, got %T", value)
		}
		*dest = resolved
		return nil
	case *string:
		resolved, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string value, got %T", value)
		}
		*dest = resolved
		return nil
	case *bool:
		resolved, ok := value.(bool)
		if !ok {
			return fmt.Errorf("expected bool value, got %T", value)
		}
		*dest = resolved
		return nil
	case *sql.NullString:
		switch resolved := value.(type) {
		case nil:
			*dest = sql.NullString{}
			return nil
		case sql.NullString:
			*dest = resolved
			return nil
		case string:
			*dest = sql.NullString{String: resolved, Valid: true}
			return nil
		default:
			return fmt.Errorf("expected sql.NullString-compatible value, got %T", value)
		}
	default:
		return fmt.Errorf("unsupported scan destination %T", destination)
	}
}
