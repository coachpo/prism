package configbundle

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	dangerousConfirmHeader        = "X-Prism-Dangerous-Confirm"
	dangerousConfirmProfileExport = "profile-export"
	previewTokenHeader            = "X-Prism-Preview-Token"
	previewTokenVersion           = 1
	previewTokenTTL               = 15 * time.Minute
	previewTokenProfileScope      = "profile_config"
)

type previewTokenClaims struct {
	Version           int       `json:"version"`
	Scope             string    `json:"scope"`
	ProfileID         *int      `json:"profile_id,omitempty"`
	BundleFingerprint string    `json:"bundle_fingerprint"`
	IssuedAt          time.Time `json:"issued_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

func requireDangerousProfileExportConfirm(r *http.Request) error {
	if strings.TrimSpace(r.Header.Get(dangerousConfirmHeader)) != dangerousConfirmProfileExport {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("%s header must be '%s'", dangerousConfirmHeader, dangerousConfirmProfileExport)}
	}
	return nil
}

func requirePreviewTokenHeader(r *http.Request) (string, error) {
	token := strings.TrimSpace(r.Header.Get(previewTokenHeader))
	if token == "" {
		return "", &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("%s header is required", previewTokenHeader)}
	}
	return token, nil
}

func profileImportBundleFingerprint(data profileImportRequest) (string, error) {
	return bundleFingerprint(data)
}

func bundleFingerprint(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal config bundle fingerprint: %w", err)
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

func (s *Service) issueProfilePreviewToken(profileID int, fingerprint string) (string, error) {
	now := s.nowUTC()
	return s.issuePreviewToken(previewTokenClaims{Version: previewTokenVersion, Scope: previewTokenProfileScope, ProfileID: &profileID, BundleFingerprint: fingerprint, IssuedAt: now, ExpiresAt: now.Add(previewTokenTTL)})
}

func (s *Service) validateProfilePreviewToken(token string, profileID int, data profileImportRequest) error {
	fingerprint, err := profileImportBundleFingerprint(data)
	if err != nil {
		return err
	}
	claims, err := s.parsePreviewToken(token)
	if err != nil {
		return err
	}
	if claims.Scope != previewTokenProfileScope || claims.ProfileID == nil || *claims.ProfileID != profileID || claims.BundleFingerprint != fingerprint || s.nowUTC().After(claims.ExpiresAt) {
		return invalidPreviewTokenError()
	}
	return nil
}

func (s *Service) issuePreviewToken(claims previewTokenClaims) (string, error) {
	key, err := s.resolvedPreviewTokenKey()
	if err != nil {
		return "", err
	}
	rawClaims, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal preview token claims: %w", err)
	}
	signature := signPreviewToken(rawClaims, key)
	return base64.RawURLEncoding.EncodeToString(rawClaims) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (s *Service) parsePreviewToken(token string) (previewTokenClaims, error) {
	key, err := s.resolvedPreviewTokenKey()
	if err != nil {
		return previewTokenClaims{}, err
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return previewTokenClaims{}, invalidPreviewTokenError()
	}
	rawClaims, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return previewTokenClaims{}, invalidPreviewTokenError()
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return previewTokenClaims{}, invalidPreviewTokenError()
	}
	if !hmac.Equal(signature, signPreviewToken(rawClaims, key)) {
		return previewTokenClaims{}, invalidPreviewTokenError()
	}
	var claims previewTokenClaims
	if err := json.Unmarshal(rawClaims, &claims); err != nil {
		return previewTokenClaims{}, invalidPreviewTokenError()
	}
	if claims.Version != previewTokenVersion || strings.TrimSpace(claims.Scope) == "" || strings.TrimSpace(claims.BundleFingerprint) == "" || claims.ExpiresAt.IsZero() {
		return previewTokenClaims{}, invalidPreviewTokenError()
	}
	return claims, nil
}

func signPreviewToken(value []byte, key string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(value)
	return mac.Sum(nil)
}

func invalidPreviewTokenError() error {
	return &domainError{StatusCode: http.StatusConflict, Detail: "Preview token is invalid, expired, or does not match this bundle. Run preview again and retry."}
}
