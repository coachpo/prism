package configbundle

import (
	"testing"
	"time"

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
