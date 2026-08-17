package settings

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const currencyDraftDefaultChunkLimit = 100

const currencyDraftMaxChunkLimit = 500

const currencyDraftPreviewLimit = 50

const currencyDraftMaxPreviewLimit = 100

type currencyDraftCursor struct {
	Version     int    `json:"v"`
	ProfileID   int    `json:"profile"`
	DraftID     string `json:"draft"`
	Kind        string `json:"kind"`
	Binding     string `json:"binding"`
	LastOrdinal int    `json:"last_ordinal,omitempty"`
	LastID      int    `json:"last_id,omitempty"`
}

func currencyDraftPageLimit(r *http.Request, defaultLimit, maxLimit int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxLimit {
		return 0, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("limit must be between 1 and %d", maxLimit)}
	}
	return limit, nil
}

func (s *Service) encodeCurrencyDraftCursor(cursor currencyDraftCursor) string {
	cursor.Version = 1
	payload, _ := json.Marshal(cursor)
	mac := hmac.New(sha256.New, s.currencyCursorKey)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
}

func (s *Service) decodeCurrencyDraftCursor(raw string, expected currencyDraftCursor) (currencyDraftCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return currencyDraftCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) <= sha256.Size {
		return currencyDraftCursor{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "currency migration cursor is invalid"}
	}
	payload, signature := decoded[:len(decoded)-sha256.Size], decoded[len(decoded)-sha256.Size:]
	mac := hmac.New(sha256.New, s.currencyCursorKey)
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return currencyDraftCursor{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "currency migration cursor is invalid"}
	}
	var cursor currencyDraftCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Version != 1 || cursor.ProfileID != expected.ProfileID || cursor.DraftID != expected.DraftID || cursor.Kind != expected.Kind || cursor.Binding != expected.Binding {
		return currencyDraftCursor{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "currency migration cursor is stale"}
	}
	return cursor, nil
}
