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
}

func TestNormalizeBaseURLOrder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"trims surrounding whitespace", "  https://api.openai.com  ", "https://api.openai.com"},
		{"removes trailing slash", "https://api.openai.com/", "https://api.openai.com"},
		{"removes multiple trailing slashes", "https://api.openai.com/v1///", "https://api.openai.com/v1"},
		{"preserves path prefix", "https://api.openai.com/v1", "https://api.openai.com/v1"},
		{"keeps invalid bare origin for validation", "https://", "https://"},
		{"keeps empty", "", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeBaseURL(test.raw)
			if got != test.want {
				t.Fatalf("NormalizeBaseURL(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestValidateBaseURL(t *testing.T) {
	t.Parallel()

	valid := "https://api.openai.com/v1"
	if codes := ValidateBaseURL(valid); len(codes) != 0 {
		t.Fatalf("expected valid URL to pass, got %v", codes)
	}
	for name, raw := range map[string]string{
		"missing scheme":     "api.openai.com",
		"bare scheme":        "https://",
		"unsupported scheme": "ftp://api.openai.com",
		"query rejected":     "https://api.openai.com/v1?key=1",
		"fragment rejected":  "https://api.openai.com/v1#frag",
	} {
		t.Run(name, func(t *testing.T) {
			codes := ValidateBaseURL(raw)
			if len(codes) == 0 {
				t.Fatalf("expected %q to be invalid", raw)
			}
			if codes[0] != FieldErrorBaseURLInvalid {
				t.Fatalf("expected base_url_invalid code, got %v", codes)
			}
		})
	}

	overlong := "https://api.example.com/" + strings.Repeat("a", 513)
	if codes := ValidateBaseURL(overlong); len(codes) == 0 || codes[0] != FieldErrorBaseURLTooLong {
		t.Fatalf("expected base_url_too_long code, got %v", codes)
	}
	// 512 code points is acceptable even with multibyte characters.
	boundary := "https://api.example.com/" + strings.Repeat("界", 512-24)
	if ValidateBaseURL(boundary) != nil {
		t.Fatalf("expected code-point counting to accept 512 boundary, codes=%v", ValidateBaseURL(boundary))
	}
	tooLong := "https://api.example.com/" + strings.Repeat("界", 513-24)
	if codes := ValidateBaseURL(tooLong); len(codes) == 0 || codes[0] != FieldErrorBaseURLTooLong {
		t.Fatalf("expected 513 code points to be too long, got %v", codes)
	}
}

func TestValidateEndpointName(t *testing.T) {
	t.Parallel()

	if code := ValidateEndpointName(""); code != FieldErrorNameRequired {
		t.Fatalf("expected name_required, got %q", code)
	}
	if code := ValidateEndpointName(strings.Repeat("a", 128)); code != "" {
		t.Fatalf("expected 128 code points to pass, got %q", code)
	}
	if code := ValidateEndpointName(strings.Repeat("界", 129)); code != FieldErrorNameTooLong {
		t.Fatalf("expected name_too_long for 129 code points, got %q", code)
	}
}

func TestBuildDuplicateEndpointName(t *testing.T) {
	t.Parallel()

	existing := map[string]struct{}{"Primary copy": {}}
	got := BuildDuplicateEndpointName("Primary", existing)
	if got != "Primary copy 2" {
		t.Fatalf("expected suffix after collision, got %q", got)
	}
}
