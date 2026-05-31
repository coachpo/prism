package configbundle

import (
	"net/http"
	"testing"
	"time"
)

func TestPreviewTokenProfileValidationTracksFingerprintAndTTL(t *testing.T) {
	issuedAt := time.Unix(1700000000, 0).UTC()
	service := newPreviewTokenTestService(issuedAt)
	request := validProfileBundleV3Request()

	fingerprint, err := profileImportBundleFingerprint(request)
	if err != nil {
		t.Fatalf("profile fingerprint: %v", err)
	}
	token, err := service.issueProfilePreviewToken(42, fingerprint)
	if err != nil {
		t.Fatalf("issue profile preview token: %v", err)
	}
	claims, err := service.parsePreviewToken(token)
	if err != nil {
		t.Fatalf("parse profile preview token: %v", err)
	}
	if claims.Scope != previewTokenProfileScope || claims.ProfileID == nil || *claims.ProfileID != 42 {
		t.Fatalf("expected profile-scoped claims, got %+v", claims)
	}
	if !claims.IssuedAt.Equal(issuedAt) || !claims.ExpiresAt.Equal(issuedAt.Add(previewTokenTTL)) {
		t.Fatalf("expected deterministic issued/expires timestamps, got issued=%s expires=%s", claims.IssuedAt, claims.ExpiresAt)
	}
	if err := service.validateProfilePreviewToken(token, 42, request); err != nil {
		t.Fatalf("validate matching profile token: %v", err)
	}
}

func TestPreviewTokenVendorValidationRejectsScopeProfileAndBundleMismatch(t *testing.T) {
	issuedAt := time.Unix(1700000100, 0).UTC()
	service := newPreviewTokenTestService(issuedAt)
	profileRequest := validProfileBundleV3Request()
	vendorRequest := vendorCatalogImportRequest{
		Version:    canonicalVendorCatalogVersion,
		BundleKind: canonicalVendorCatalogKind,
		Vendors:    []vendorCatalogRow{{Key: "openai", Name: "OpenAI", AuditCaptureBodies: true}},
	}

	profileFingerprint, err := profileImportBundleFingerprint(profileRequest)
	if err != nil {
		t.Fatalf("profile fingerprint: %v", err)
	}
	profileToken, err := service.issueProfilePreviewToken(42, profileFingerprint)
	if err != nil {
		t.Fatalf("issue profile token: %v", err)
	}
	vendorFingerprint, err := vendorCatalogImportBundleFingerprint(vendorRequest)
	if err != nil {
		t.Fatalf("vendor fingerprint: %v", err)
	}
	vendorToken, err := service.issueVendorPreviewToken(vendorFingerprint)
	if err != nil {
		t.Fatalf("issue vendor token: %v", err)
	}
	if err := service.validateVendorPreviewToken(vendorToken, vendorRequest); err != nil {
		t.Fatalf("validate matching vendor token: %v", err)
	}

	mutatedProfile := validProfileBundleV3Request()
	mutatedProfile.Models[0].ModelID = "gpt-4o-mutated"
	mutatedVendor := vendorRequest
	mutatedVendor.Vendors[0].Name = "OpenAI Mutated"

	requireConfigBundleDomainError(t, service.validateProfilePreviewToken(profileToken, 7, profileRequest), http.StatusConflict, invalidPreviewTokenError().Error())
	requireConfigBundleDomainError(t, service.validateProfilePreviewToken(vendorToken, 42, profileRequest), http.StatusConflict, invalidPreviewTokenError().Error())
	requireConfigBundleDomainError(t, service.validateProfilePreviewToken(profileToken, 42, mutatedProfile), http.StatusConflict, invalidPreviewTokenError().Error())
	requireConfigBundleDomainError(t, service.validateVendorPreviewToken(profileToken, vendorRequest), http.StatusConflict, invalidPreviewTokenError().Error())
	requireConfigBundleDomainError(t, service.validateVendorPreviewToken(vendorToken, mutatedVendor), http.StatusConflict, invalidPreviewTokenError().Error())

	expiredService := newPreviewTokenTestService(issuedAt.Add(previewTokenTTL + time.Second))
	requireConfigBundleDomainError(t, expiredService.validateProfilePreviewToken(profileToken, 42, profileRequest), http.StatusConflict, invalidPreviewTokenError().Error())
	requireConfigBundleDomainError(t, expiredService.validateVendorPreviewToken(vendorToken, vendorRequest), http.StatusConflict, invalidPreviewTokenError().Error())
}

func TestPreviewTokenRejectsMalformedTokens(t *testing.T) {
	issuedAt := time.Unix(1700000200, 0).UTC()
	service := newPreviewTokenTestService(issuedAt)
	request := validProfileBundleV3Request()
	fingerprint, err := profileImportBundleFingerprint(request)
	if err != nil {
		t.Fatalf("profile fingerprint: %v", err)
	}

	validToken, err := service.issueProfilePreviewToken(42, fingerprint)
	if err != nil {
		t.Fatalf("issue valid profile token: %v", err)
	}
	invalidClaimsToken, err := service.issuePreviewToken(previewTokenClaims{
		Version:           previewTokenVersion,
		BundleFingerprint: fingerprint,
		IssuedAt:          issuedAt,
		ExpiresAt:         issuedAt.Add(previewTokenTTL),
	})
	if err != nil {
		t.Fatalf("issue invalid-claims token: %v", err)
	}

	tests := []struct {
		name  string
		token string
	}{
		{name: "missing separator", token: "not-a-token"},
		{name: "tampered signature", token: validToken + "x"},
		{name: "missing scope claim", token: invalidClaimsToken},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.parsePreviewToken(test.token)
			requireConfigBundleDomainError(t, err, http.StatusConflict, invalidPreviewTokenError().Error())
		})
	}
}

func newPreviewTokenTestService(now time.Time) *Service {
	return &Service{
		now:             func() time.Time { return now },
		previewTokenKey: "bundle-secret-key",
	}
}
