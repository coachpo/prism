package stats

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const costSegmentCursorSigningDomain = "prism.observe.cost-segment-cursor.v1"

type costSegmentCursorPayload struct {
	Version        int    `json:"v"`
	ProfileID      int    `json:"p"`
	LastSegmentKey string `json:"k"`
	Consumed       int    `json:"c"`
}

func encodeCostSegmentCursor(payload costSegmentCursorPayload, signingKey []byte) (string, error) {
	if len(signingKey) == 0 {
		return "", fmt.Errorf("cost segment cursor signing key is unavailable")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signature := signCostSegmentCursor(raw, signingKey)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func decodeCostSegmentCursor(encoded string, signingKey []byte) (costSegmentCursorPayload, error) {
	if len(signingKey) == 0 {
		return costSegmentCursorPayload{}, fmt.Errorf("cost segment cursor signing key is unavailable")
	}
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return costSegmentCursorPayload{}, fmt.Errorf("invalid cost segment cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return costSegmentCursorPayload{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return costSegmentCursorPayload{}, err
	}
	if !hmac.Equal(signature, signCostSegmentCursor(raw, signingKey)) {
		return costSegmentCursorPayload{}, fmt.Errorf("invalid cost segment cursor signature")
	}
	var payload costSegmentCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return costSegmentCursorPayload{}, err
	}
	if payload.Version != 1 {
		return costSegmentCursorPayload{}, fmt.Errorf("unsupported cost segment cursor version")
	}
	return payload, nil
}

// DeriveCostSegmentCursorSigningKey derives a domain-separated HMAC subkey
// from the server secret encryption key, without using that secret directly.
func DeriveCostSegmentCursorSigningKey(secretEncryptionKey string) []byte {
	if strings.TrimSpace(secretEncryptionKey) == "" {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(secretEncryptionKey))
	_, _ = mac.Write([]byte(costSegmentCursorSigningDomain))
	return mac.Sum(nil)
}

func signCostSegmentCursor(raw []byte, signingKey []byte) []byte {
	mac := hmac.New(sha256.New, signingKey)
	_, _ = mac.Write([]byte(costSegmentCursorSigningDomain + "\x00"))
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}
