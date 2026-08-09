package endpointdomain

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const encryptedSecretPrefix = "enc:"

const (
	// MaxEndpointNameCodePoints is the create/update contract for endpoint names
	// after trimming surrounding whitespace (counted in Unicode code points).
	MaxEndpointNameCodePoints = 128
	// MaxEndpointBaseURLCodePoints is the create/update contract for normalized
	// base URLs (counted in Unicode code points).
	MaxEndpointBaseURLCodePoints = 512
)

const fingerprintDomain = "prism:endpoint-api-key-fingerprint:v1"

// Stable field error codes returned by validation helpers. The management layer
// maps them into typed 422 `fields` payloads; the frontend owns zh-CN copy.
const (
	FieldErrorNameRequired    = "name_required"
	FieldErrorNameTooLong     = "name_too_long"
	FieldErrorBaseURLInvalid  = "base_url_invalid"
	FieldErrorBaseURLTooLong  = "base_url_too_long"
)

// NormalizeBaseURL applies the §5.2 normalization order: trim surrounding
// whitespace, then remove trailing '/' characters while preserving a valid
// origin form. Values that fail to parse after slash removal keep their
// whitespace-trimmed form so validation can report the original problem.
func NormalizeBaseURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	withoutSlash := strings.TrimRight(trimmed, "/")
	parsed, err := url.Parse(withoutSlash)
	if err == nil && strings.TrimSpace(parsed.Scheme) != "" && strings.TrimSpace(parsed.Host) != "" {
		return withoutSlash
	}
	return trimmed
}

// ValidateBaseURL returns stable error codes for a normalized base URL.
// It requires an http/https scheme and a host, rejects query/fragment, and
// enforces the normalized 512 code-point limit.
func ValidateBaseURL(baseURL string) []string {
	var codes []string
	parsed, err := url.Parse(baseURL)
	scheme := ""
	host := ""
	if err == nil {
		scheme = strings.TrimSpace(parsed.Scheme)
		host = strings.TrimSpace(parsed.Host)
	}
	if scheme == "" || host == "" || (scheme != "http" && scheme != "https") {
		codes = append(codes, FieldErrorBaseURLInvalid)
	}
	if parsed != nil && (parsed.RawQuery != "" || parsed.Fragment != "") {
		codes = append(codes, FieldErrorBaseURLInvalid)
	}
	if utf8.RuneCountInString(baseURL) > MaxEndpointBaseURLCodePoints {
		codes = append(codes, FieldErrorBaseURLTooLong)
	}
	return codes
}

// ValidateEndpointName returns a stable error code for a trimmed endpoint name,
// or "" when valid. Names are required and limited to 128 Unicode code points.
func ValidateEndpointName(name string) string {
	if name == "" {
		return FieldErrorNameRequired
	}
	if utf8.RuneCountInString(name) > MaxEndpointNameCodePoints {
		return FieldErrorNameTooLong
	}
	return ""
}

// APIKeyFingerprint derives the instance-local, domain-separated display
// fingerprint for a trimmed plaintext API key: "fp_v1_" + 12 lowercase hex
// characters (48 bits). It is deterministic for the same instance key and
// plaintext, never derived from ciphertext, and must never be used for
// authorization or equality decisions.
func APIKeyFingerprint(secretEncryptionKey string, plaintext string) string {
	digest := apiKeyDigest(secretEncryptionKey, plaintext)
	return "fp_v1_" + hex.EncodeToString(digest[:6])
}

// fingerprintInput applies the §4.3 definition S = strings.TrimSpace(raw key).
func fingerprintInput(value string) string {
	return strings.TrimSpace(value)
}

// APIKeyDigest returns the full HMAC-SHA256 identity digest for a trimmed
// plaintext API key under the instance secret-encryption key. The full digest
// (not the 48-bit display token) is the equality basis for key identity.
func APIKeyDigest(secretEncryptionKey string, plaintext string) [sha256.Size]byte {
	return apiKeyDigest(secretEncryptionKey, plaintext)
}

// APIKeyIdentityMatches reports whether two trimmed plaintext API keys share
// the same identity using constant-time comparison of full HMAC digests.
// Display-token collisions never affect identity.
func APIKeyIdentityMatches(secretEncryptionKey string, plaintextA string, plaintextB string) bool {
	digestA := apiKeyDigest(secretEncryptionKey, plaintextA)
	digestB := apiKeyDigest(secretEncryptionKey, plaintextB)
	return hmac.Equal(digestA[:], digestB[:])
}

func apiKeyDigest(secretEncryptionKey string, plaintext string) [sha256.Size]byte {
	normalized := fingerprintInput(plaintext)
	rootKey := sha256.Sum256([]byte(secretEncryptionKey))
	fingerprintKey := hmac.New(sha256.New, rootKey[:])
	fingerprintKey.Write([]byte(fingerprintDomain))
	digest := hmac.New(sha256.New, fingerprintKey.Sum(nil))
	digest.Write([]byte(normalized))
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

// SecretMetadata is the write-time secret contract shared by every Endpoint
// creation path (create, duplicate, model-scoped inline create).
type SecretMetadata struct {
	EncryptedValue   string
	Fingerprint      *string
	KeyUpdatedAt     *time.Time
}

// BuildSecretMetadata computes the at-rest encrypted value plus fingerprint and
// key time for a raw API key. Blank/whitespace keys produce an empty encrypted
// value with null fingerprint/time. nowUTC is used for key-time assignment.
func BuildSecretMetadata(rawKey string, secretEncryptionKey string, nowUTC func() time.Time) (SecretMetadata, error) {
	normalized := strings.TrimSpace(rawKey)
	metadata := SecretMetadata{}
	if normalized == "" {
		return metadata, nil
	}
	now := nowUTC()
	encrypted, err := EncryptSecret(normalized, secretEncryptionKey, func() time.Time { return now })
	if err != nil {
		return metadata, err
	}
	fingerprint := APIKeyFingerprint(secretEncryptionKey, normalized)
	metadata = SecretMetadata{
		EncryptedValue: encrypted,
		Fingerprint:    &fingerprint,
		KeyUpdatedAt:   &now,
	}
	return metadata, nil
}

// BuildDuplicateEndpointName derives the next free "<name> copy [N]" name.
func BuildDuplicateEndpointName(sourceName string, existingNames map[string]struct{}) string {
	baseName := strings.TrimSpace(sourceName) + " copy"
	if _, exists := existingNames[baseName]; !exists {
		return baseName
	}

	suffix := 2
	for {
		candidate := fmt.Sprintf("%s %d", baseName, suffix)
		if _, exists := existingNames[candidate]; !exists {
			return candidate
		}
		suffix++
	}
}

// HasAPIKey reports whether the stored (possibly encrypted) value carries a
// non-blank secret.
func HasAPIKey(value string) bool {
	return strings.TrimSpace(value) != ""
}

// EncryptSecret stores a trimmed plaintext secret at-rest using an AES-CBC
// Fernet-like envelope with a random IV and HMAC signature. Already-encrypted
// values pass through unchanged, which keeps startup normalization idempotent.
func EncryptSecret(value string, rawKey string, now func() time.Time) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", nil
	}
	if strings.HasPrefix(normalized, encryptedSecretPrefix) {
		return normalized, nil
	}
	if strings.TrimSpace(rawKey) == "" {
		return "", fmt.Errorf("secret encryption key is required")
	}
	if now == nil {
		now = time.Now
	}

	derivedKey := sha256.Sum256([]byte(rawKey))
	signingKey := derivedKey[:16]
	encryptionKey := derivedKey[16:]
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("build AES cipher: %w", err)
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("generate fernet IV: %w", err)
	}

	padded := pkcs7Pad([]byte(normalized), aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	payload := bytes.NewBuffer(make([]byte, 0, 1+8+len(iv)+len(ciphertext)+sha256.Size))
	payload.WriteByte(0x80)
	var timestamp [8]byte
	binary.BigEndian.PutUint64(timestamp[:], uint64(now().UTC().Unix()))
	payload.Write(timestamp[:])
	payload.Write(iv)
	payload.Write(ciphertext)

	mac := hmac.New(sha256.New, signingKey)
	mac.Write(payload.Bytes())
	payload.Write(mac.Sum(nil))
	return encryptedSecretPrefix + base64.URLEncoding.EncodeToString(payload.Bytes()), nil
}

// DecryptSecret returns the plaintext for a stored secret. Legacy plaintext
// values pass through unchanged so pre-migration rows can be normalized.
func DecryptSecret(value string, rawKey string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", nil
	}
	if !strings.HasPrefix(normalized, encryptedSecretPrefix) {
		return normalized, nil
	}
	if strings.TrimSpace(rawKey) == "" {
		return "", fmt.Errorf("secret encryption key is required")
	}

	tokenBytes, err := base64.URLEncoding.DecodeString(strings.TrimPrefix(normalized, encryptedSecretPrefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted secret: %w", err)
	}
	if len(tokenBytes) <= 1+8+aes.BlockSize+sha256.Size {
		return "", fmt.Errorf("encrypted secret payload is invalid")
	}
	derivedKey := sha256.Sum256([]byte(rawKey))
	signingKey := derivedKey[:16]
	encryptionKey := derivedKey[16:]

	payload := tokenBytes[:len(tokenBytes)-sha256.Size]
	signature := tokenBytes[len(tokenBytes)-sha256.Size:]
	mac := hmac.New(sha256.New, signingKey)
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", fmt.Errorf("encrypted secret signature is invalid")
	}
	if payload[0] != 0x80 {
		return "", fmt.Errorf("encrypted secret version is invalid")
	}

	ivStart := 1 + 8
	cipherStart := ivStart + aes.BlockSize
	ciphertext := payload[cipherStart:]
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("encrypted secret ciphertext is invalid")
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("build AES cipher: %w", err)
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, payload[ivStart:cipherStart]).CryptBlocks(plaintext, ciphertext)
	unpadded, err := pkcs7Unpad(plaintext, aes.BlockSize)
	if err != nil {
		return "", err
	}
	return string(unpadded), nil
}

func pkcs7Pad(value []byte, blockSize int) []byte {
	paddingSize := blockSize - (len(value) % blockSize)
	if paddingSize == 0 {
		paddingSize = blockSize
	}
	padding := bytes.Repeat([]byte{byte(paddingSize)}, paddingSize)
	return append(value, padding...)
}

func pkcs7Unpad(value []byte, blockSize int) ([]byte, error) {
	if len(value) == 0 || len(value)%blockSize != 0 {
		return nil, fmt.Errorf("encrypted secret padding is invalid")
	}
	paddingSize := int(value[len(value)-1])
	if paddingSize < 1 || paddingSize > blockSize || paddingSize > len(value) {
		return nil, fmt.Errorf("encrypted secret padding is invalid")
	}
	for _, part := range value[len(value)-paddingSize:] {
		if int(part) != paddingSize {
			return nil, fmt.Errorf("encrypted secret padding is invalid")
		}
	}
	return value[:len(value)-paddingSize], nil
}
