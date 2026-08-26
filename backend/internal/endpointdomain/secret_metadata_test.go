package endpointdomain

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

const testEncryptionKey = "test-instance-secret-encryption-key"

func TestAPIKeyFingerprintTruthTable(t *testing.T) {
	t.Parallel()

	// Same plaintext -> same fixed token across calls.
	first := APIKeyFingerprint(testEncryptionKey, "sk-test-123")
	second := APIKeyFingerprint(testEncryptionKey, "sk-test-123")
	if first != second {
		t.Fatalf("expected deterministic fingerprint, got %q then %q", first, second)
	}
	if !strings.HasPrefix(first, "fp_v1_") || len(first) != 6+12 {
		t.Fatalf("expected fp_v1_ + 12 lowercase hex, got %q", first)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(first, "fp_v1_")); err != nil {
		t.Fatalf("expected lowercase hex suffix, got %q: %v", first, err)
	}
	if strings.ToLower(first) != first {
		t.Fatalf("expected lowercase fingerprint, got %q", first)
	}

	// Different plaintext -> different token.
	different := APIKeyFingerprint(testEncryptionKey, "sk-test-456")
	if first == different {
		t.Fatalf("expected different plaintext to produce a different fingerprint")
	}

	// Whitespace is trimmed before fingerprinting.
	trimmed := APIKeyFingerprint(testEncryptionKey, "  sk-test-123  ")
	if first != trimmed {
		t.Fatalf("expected surrounding whitespace to be trimmed before fingerprinting")
	}

	// Different instance key -> different token for the same plaintext.
	otherInstance := APIKeyFingerprint("other-instance-key", "sk-test-123")
	if first == otherInstance {
		t.Fatalf("expected a different instance key to produce a different fingerprint")
	}
}

func TestAPIKeyFingerprintIgnoresCiphertextReEncryption(t *testing.T) {
	t.Parallel()

	plaintext := "sk-rotate-me"
	encryptedA, err := EncryptSecret(plaintext, testEncryptionKey, func() time.Time { return time.Unix(100, 0) })
	if err != nil {
		t.Fatalf("encrypt secret A: %v", err)
	}
	encryptedB, err := EncryptSecret(plaintext, testEncryptionKey, func() time.Time { return time.Unix(200, 0) })
	if err != nil {
		t.Fatalf("encrypt secret B: %v", err)
	}
	if encryptedA == encryptedB {
		t.Fatalf("expected random IV to produce distinct ciphertexts")
	}
	decryptedA, err := DecryptSecret(encryptedA, testEncryptionKey)
	if err != nil {
		t.Fatalf("decrypt A: %v", err)
	}
	decryptedB, err := DecryptSecret(encryptedB, testEncryptionKey)
	if err != nil {
		t.Fatalf("decrypt B: %v", err)
	}
	fingerprintA := APIKeyFingerprint(testEncryptionKey, decryptedA)
	fingerprintB := APIKeyFingerprint(testEncryptionKey, decryptedB)
	if fingerprintA != fingerprintB {
		t.Fatalf("expected ciphertext re-encryption to preserve fingerprint, got %q vs %q", fingerprintA, fingerprintB)
	}
}

func TestAPIKeyIdentityCompare(t *testing.T) {
	t.Parallel()

	key := "sk-identity-1"
	digest := APIKeyDigest(testEncryptionKey, key)
	if len(digest) != 32 {
		t.Fatalf("expected full 32-byte digest, got %d", len(digest))
	}
	if !APIKeyIdentityMatches(testEncryptionKey, key, key) {
		t.Fatalf("expected the same key to match identity")
	}
	if APIKeyIdentityMatches(testEncryptionKey, key, key+"-different") {
		t.Fatalf("expected different keys not to match identity")
	}
	// Display-token collisions never affect identity: a 48-bit suffix match
	// is not the equality basis (the full digest comparison is).
	if !APIKeyIdentityMatches(testEncryptionKey, key, key) {
		t.Fatalf("expected same-key identity to match")
	}
}

func TestBuildSecretMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 10, 20, 30, 0, time.UTC)
	nowFn := func() time.Time { return now }

	// Blank key -> empty encrypted value, null fingerprint and key time.
	blank, err := BuildSecretMetadata("   ", testEncryptionKey, nowFn)
	if err != nil {
		t.Fatalf("build blank metadata: %v", err)
	}
	if blank.EncryptedValue != "" || blank.Fingerprint != nil || blank.KeyUpdatedAt != nil {
		t.Fatalf("expected blank key metadata to be empty, got %+v", blank)
	}

	// Non-blank key -> encrypted + fingerprint + key time.
	metadata, err := BuildSecretMetadata("sk-metadata", testEncryptionKey, nowFn)
	if err != nil {
		t.Fatalf("build metadata: %v", err)
	}
	if metadata.EncryptedValue == "" || !strings.HasPrefix(metadata.EncryptedValue, "enc:") {
		t.Fatalf("expected encrypted value, got %q", metadata.EncryptedValue)
	}
	if metadata.Fingerprint == nil || *metadata.Fingerprint != APIKeyFingerprint(testEncryptionKey, "sk-metadata") {
		t.Fatalf("expected fingerprint to match plaintext derivation, got %v", metadata.Fingerprint)
	}
	if metadata.KeyUpdatedAt == nil || !metadata.KeyUpdatedAt.Equal(now) {
		t.Fatalf("expected key time %v, got %v", now, metadata.KeyUpdatedAt)
	}
	plaintext, err := DecryptSecret(metadata.EncryptedValue, testEncryptionKey)
	if err != nil {
		t.Fatalf("decrypt built secret: %v", err)
	}
	if plaintext != "sk-metadata" {
		t.Fatalf("expected round-trip plaintext, got %q", plaintext)
	}

	// A legitimate operator key may begin with the storage prefix. Creation
	// paths must still encrypt it; only migration normalization treats an
	// already-enveloped value as ciphertext.
	prefixed, err := BuildSecretMetadata("enc:operator-key", testEncryptionKey, nowFn)
	if err != nil {
		t.Fatalf("build prefixed metadata: %v", err)
	}
	if prefixed.EncryptedValue == "enc:operator-key" {
		t.Fatal("expected a plaintext key with the envelope prefix to be encrypted")
	}
	prefixedPlaintext, err := DecryptSecret(prefixed.EncryptedValue, testEncryptionKey)
	if err != nil {
		t.Fatalf("decrypt prefixed key: %v", err)
	}
	if prefixedPlaintext != "enc:operator-key" {
		t.Fatalf("expected prefixed key round-trip, got %q", prefixedPlaintext)
	}
}
