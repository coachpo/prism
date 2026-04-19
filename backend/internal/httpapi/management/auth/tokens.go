package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	proxyAPIKeyPrefix       = "pm-"
	proxyAPIKeyLookupLength = 8
)

type sessionDuration string

const (
	sessionDurationSession   sessionDuration = "session"
	sessionDurationSevenDays sessionDuration = "7_days"
	sessionDurationThirtyDay sessionDuration = "30_days"
)

type accessTokenClaims struct {
	Sub          string `json:"sub"`
	Username     string `json:"username"`
	TokenVersion int    `json:"token_version"`
	Type         string `json:"type"`
	IssuedAt     int64  `json:"iat"`
	ExpiresAt    int64  `json:"exp"`
	JTI          string `json:"jti"`
}

func normalizeSessionDuration(value string) (sessionDuration, error) {
	switch strings.TrimSpace(value) {
	case "", string(sessionDurationSevenDays):
		return sessionDurationSevenDays, nil
	case string(sessionDurationSession):
		return sessionDurationSession, nil
	case string(sessionDurationThirtyDay):
		return sessionDurationThirtyDay, nil
	default:
		return "", errors.New("invalid session duration")
	}
}

func (duration sessionDuration) refreshExpiry(now time.Time, fallbackTTL time.Duration) time.Time {
	switch duration {
	case sessionDurationSession:
		return now.Add(fallbackTTL)
	case sessionDurationThirtyDay:
		return now.Add(30 * 24 * time.Hour)
	default:
		return now.Add(7 * 24 * time.Hour)
	}
}

func (duration sessionDuration) accessCookieMaxAge(accessTTL time.Duration) int {
	if duration == sessionDurationSession {
		return 0
	}
	return int(accessTTL / time.Second)
}

func (duration sessionDuration) refreshCookieMaxAge(now time.Time, expiresAt time.Time) int {
	if duration == sessionDurationSession {
		return 0
	}
	remaining := int(expiresAt.Sub(now).Seconds())
	if remaining < 0 {
		return 0
	}
	return remaining
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func verifyPassword(password string, passwordHash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
}

func hashOpaqueToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func verifyOpaqueToken(value string, expectedHash string) bool {
	return hmac.Equal([]byte(hashOpaqueToken(value)), []byte(expectedHash))
}

func generateOTPCode() (string, error) {
	maxValue := big.NewInt(1_000_000)
	value, err := rand.Int(rand.Reader, maxValue)
	if err != nil {
		return "", fmt.Errorf("generate otp code: %w", err)
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func randomTokenURLSafe(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read random token bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func buildRefreshTokenRecord(expiresAt time.Time) (string, string, time.Time, error) {
	rawToken, err := randomTokenURLSafe(48)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return rawToken, hashOpaqueToken(rawToken), expiresAt, nil
}

func buildProxyAPIKey() (string, string, string, string, error) {
	lookup, err := randomHex(proxyAPIKeyLookupLength / 2)
	if err != nil {
		return "", "", "", "", err
	}
	secret, err := randomHex(12)
	if err != nil {
		return "", "", "", "", err
	}
	keyPrefix := proxyAPIKeyPrefix + lookup
	rawKey := keyPrefix + secret
	return rawKey, keyPrefix, rawKey[len(rawKey)-4:], hashOpaqueToken(rawKey), nil
}

func parseProxyAPIKey(rawKey string) (string, string, error) {
	normalized := strings.TrimSpace(rawKey)
	prefixLength := len(proxyAPIKeyPrefix) + proxyAPIKeyLookupLength
	if strings.HasPrefix(normalized, proxyAPIKeyPrefix) && len(normalized) > prefixLength {
		return normalized, normalized[:prefixLength], nil
	}
	if strings.Contains(normalized, "_") {
		compatiblePrefix, _, found := strings.Cut(normalized, "_")
		if found && compatiblePrefix != "" {
			return normalized, compatiblePrefix, nil
		}
	}
	return "", "", errors.New("invalid proxy api key format")
}

func createAccessToken(now time.Time, ttl time.Duration, secret string, subjectID int, username string, tokenVersion int) (string, error) {
	claims := accessTokenClaims{
		Sub:          strconv.Itoa(subjectID),
		Username:     username,
		TokenVersion: tokenVersion,
		Type:         "access",
		IssuedAt:     now.Unix(),
		ExpiresAt:    now.Add(ttl).Unix(),
	}
	jti, err := randomHex(16)
	if err != nil {
		return "", err
	}
	claims.JTI = jti
	return encodeJWT(claims, secret)
}

func parseAccessToken(now time.Time, secret string, token string) (accessTokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return accessTokenClaims{}, errors.New("invalid jwt format")
	}

	signingInput := parts[0] + "." + parts[1]
	expectedSignature := signJWT(signingInput, secret)
	if !hmac.Equal([]byte(expectedSignature), []byte(parts[2])) {
		return accessTokenClaims{}, errors.New("invalid jwt signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return accessTokenClaims{}, fmt.Errorf("decode jwt payload: %w", err)
	}

	var claims accessTokenClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return accessTokenClaims{}, fmt.Errorf("decode jwt claims: %w", err)
	}
	if claims.Type != "access" {
		return accessTokenClaims{}, errors.New("invalid token type")
	}
	if claims.ExpiresAt < now.Unix() {
		return accessTokenClaims{}, errors.New("expired access token")
	}
	return claims, nil
}

func encodeJWT(claims accessTokenClaims, secret string) (string, error) {
	headerBytes, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("encode jwt header: %w", err)
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode jwt payload: %w", err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerBytes)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := encodedHeader + "." + encodedPayload
	return signingInput + "." + signJWT(signingInput, secret), nil
}

func signJWT(signingInput string, secret string) string {
	signer := hmac.New(sha256.New, []byte(secret))
	_, _ = signer.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(signer.Sum(nil))
}
