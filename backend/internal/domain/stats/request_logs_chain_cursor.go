package stats

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// chainCursorKey signs chain/row cursors with a fixed local key.
const chainCursorKey = "prism-chain-cursor-v1"

type chainCursorPayload struct {
	Version             int    `json:"v"`
	ProfileID           int    `json:"p"`
	OrderAt             string `json:"o"`
	IngressID           string `json:"i"`
	UsageEventID        int64  `json:"u"`
	Limit               int    `json:"l"`
	SortOrder           string `json:"s"`
	RetentionEpoch      int64  `json:"r"`
	RetentionGeneration int64  `json:"g"`
}

type rowCursorPayload struct {
	Version             int    `json:"v"`
	ProfileID           int    `json:"p"`
	IngressID           string `json:"i"`
	OrderAt             string `json:"o"`
	RequestLogID        string `json:"id"`
	Limit               int    `json:"l"`
	RetentionEpoch      int64  `json:"r"`
	RetentionGeneration int64  `json:"g"`
}

// encodeChainCursor signs and encodes an outer chain cursor.
func encodeChainCursor(payload chainCursorPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signature := signChainCursor(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func decodeChainCursor(encoded string) (chainCursorPayload, error) {
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return chainCursorPayload{}, fmt.Errorf("invalid chain cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return chainCursorPayload{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return chainCursorPayload{}, err
	}
	if !hmac.Equal(signature, signChainCursor(raw)) {
		return chainCursorPayload{}, fmt.Errorf("invalid chain cursor signature")
	}
	var payload chainCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return chainCursorPayload{}, err
	}
	if payload.Version != 1 {
		return chainCursorPayload{}, fmt.Errorf("unsupported chain cursor version")
	}
	return payload, nil
}

func signChainCursor(raw []byte) []byte {
	mac := hmac.New(sha256.New, []byte(chainCursorKey))
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}

func encodeRowCursor(payload rowCursorPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signature := signRowCursor(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func decodeRowCursor(encoded string) (rowCursorPayload, error) {
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return rowCursorPayload{}, fmt.Errorf("invalid row cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return rowCursorPayload{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return rowCursorPayload{}, err
	}
	if !hmac.Equal(signature, signRowCursor(raw)) {
		return rowCursorPayload{}, fmt.Errorf("invalid row cursor signature")
	}
	var payload rowCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return rowCursorPayload{}, err
	}
	if payload.Version != 1 {
		return rowCursorPayload{}, fmt.Errorf("unsupported row cursor version")
	}
	if strings.TrimSpace(payload.IngressID) == "" || payload.ProfileID <= 0 || payload.Limit <= 0 {
		return rowCursorPayload{}, fmt.Errorf("invalid row cursor scope")
	}
	if _, err := time.Parse(time.RFC3339Nano, payload.OrderAt); err != nil {
		return rowCursorPayload{}, fmt.Errorf("invalid row cursor timestamp: %w", err)
	}
	requestLogID, err := strconv.ParseInt(payload.RequestLogID, 10, 64)
	if err != nil || requestLogID <= 0 {
		return rowCursorPayload{}, fmt.Errorf("invalid row cursor request log id")
	}
	return payload, nil
}

func signRowCursor(raw []byte) []byte {
	mac := hmac.New(sha256.New, []byte("prism-row-cursor-v1"))
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}
