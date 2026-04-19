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
	"fmt"
	"net/url"
	"strings"
	"time"
)

const encryptedSecretPrefix = "enc:"
const maskedSecretValue = "********"

func NormalizeBaseURL(rawURL string) string {
	return strings.TrimRight(rawURL, "/")
}

func ValidateBaseURL(baseURL string) []string {
	warnings := make([]string, 0, 2)
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return append(warnings, "base_url must include scheme and host (e.g. https://api.example.com/v1)")
	}

	if strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		warnings = append(warnings, "base_url must include scheme and host (e.g. https://api.example.com/v1)")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		warnings = append(warnings, "base_url must not include a query string or fragment")
	}
	return warnings
}

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

func HasAPIKey(value string) bool {
	return strings.TrimSpace(value) != ""
}

func MaskedAPIKey(value string) *string {
	if !HasAPIKey(value) {
		return nil
	}
	masked := maskedSecretValue
	return &masked
}

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
