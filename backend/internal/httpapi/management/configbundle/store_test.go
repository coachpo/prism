package configbundle

import (
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

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
