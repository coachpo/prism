package configbundle

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
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

const (
	encryptedSecretPrefix = "enc:"
	bundleSecretCipher    = "fernet-v1"
)

func buildBundleSecretKeyID(rawKey string) (string, error) {
	normalized := strings.TrimSpace(rawKey)
	if normalized == "" {
		return "", fmt.Errorf("config bundle encryption key is required")
	}
	derivedKey := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("sha256:%x", derivedKey[:]), nil
}

func encryptBundleSecret(value string, rawKey string, now func() time.Time) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", nil
	}
	if strings.HasPrefix(normalized, encryptedSecretPrefix) {
		return normalized, nil
	}
	key := strings.TrimSpace(rawKey)
	if key == "" {
		return "", fmt.Errorf("config bundle encryption key is required")
	}
	if now == nil {
		now = time.Now
	}

	derivedKey := sha256.Sum256([]byte(key))
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

func decryptBundleSecret(value string, rawKey string) (string, error) {
	return endpointdomain.DecryptSecret(value, rawKey)
}

func pkcs7Pad(value []byte, blockSize int) []byte {
	paddingSize := blockSize - (len(value) % blockSize)
	if paddingSize == 0 {
		paddingSize = blockSize
	}
	padding := bytes.Repeat([]byte{byte(paddingSize)}, paddingSize)
	return append(value, padding...)
}
