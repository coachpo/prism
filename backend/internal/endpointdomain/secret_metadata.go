package endpointdomain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const fingerprintDomain = "prism:endpoint-api-key-fingerprint:v1"

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
	EncryptedValue string
	Fingerprint    *string
	KeyUpdatedAt   *time.Time
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
	// This is a caller-supplied plaintext, even when it happens to begin with
	// the storage envelope prefix. Do not let the idempotent migration helper
	// mistake a legitimate key such as "enc:..." for an existing ciphertext.
	encrypted, err := encryptPlaintextSecret(normalized, secretEncryptionKey, func() time.Time { return now })
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

// HasAPIKey reports whether the stored (possibly encrypted) value carries a
// non-blank secret.
func HasAPIKey(value string) bool {
	return strings.TrimSpace(value) != ""
}
