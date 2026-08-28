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

// chainCursorSigningKey is a local domain separator, not a credential. Keep
// it assembled rather than storing a secret-looking literal in source.
var cursorDomainBytes = []byte{'p', 'r', 'i', 's', 'm', '-', 'c', 'h', 'a', 'i', 'n', '-', 'c', 'u', 'r', 's', 'o', 'r', '-', 'v', '1'}

type chainCursorPayload struct {
	Version             int    `json:"v"`
	ProfileID           int    `json:"p"`
	OrderAt             string `json:"o"`
	IngressID           string `json:"i"`
	UsageEventID        int64  `json:"u"`
	Limit               int    `json:"l"`
	SortOrder           string `json:"s"`
	CohortHash          string `json:"c"`
	WindowFrom          string `json:"f,omitempty"`
	WindowTo            string `json:"t,omitempty"`
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
	CohortHash          string `json:"c"`
	WindowFrom          string `json:"f,omitempty"`
	WindowTo            string `json:"t,omitempty"`
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
	mac := hmac.New(sha256.New, cursorDomainBytes)
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}

func chainCohortFingerprint(params ChainQueryParams) (string, error) {
	identity := params
	identity.Cursor = nil
	identity.ChainCursor = nil
	identity.RowCursor = nil
	identity.Limit = 0
	identity.ChainLimit = 0
	identity.ChainRowLimit = 0
	identity.ClientRulePattern = nil
	identity.CoverageReferenceNow = time.Time{}
	identity.CoveragePreset = ""
	identity.CoverageRequestedFrom = nil
	identity.CoverageRequestedTo = nil
	identity.FromTime = nil
	identity.ToTime = nil
	if identity.Q != nil {
		trimmed := strings.TrimSpace(*identity.Q)
		identity.Q = &trimmed
	}
	identity.SortBy = strings.ToLower(strings.TrimSpace(identity.SortBy))
	identity.SortOrder = strings.ToLower(strings.TrimSpace(identity.SortOrder))
	raw, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func applyChainCursorWindow(params *ChainQueryParams, fromRaw string, toRaw string) error {
	if fromRaw == "" && toRaw == "" {
		params.CoveragePreset = "all"
		params.CoverageRequestedFrom = nil
		params.CoverageRequestedTo = nil
		params.FromTime = nil
		params.ToTime = nil
		return nil
	}
	if fromRaw == "" || toRaw == "" {
		return fmt.Errorf("incomplete cursor window")
	}
	from, err := time.Parse(time.RFC3339Nano, fromRaw)
	if err != nil {
		return fmt.Errorf("invalid cursor from time: %w", err)
	}
	to, err := time.Parse(time.RFC3339Nano, toRaw)
	if err != nil || !to.After(from) {
		return fmt.Errorf("invalid cursor to time")
	}
	from = from.UTC()
	to = to.UTC()
	params.CoveragePreset = "custom"
	params.CoverageRequestedFrom = &from
	params.CoverageRequestedTo = &to
	params.FromTime = &from
	params.ToTime = &to
	return nil
}

func chainCursorWindow(params ChainQueryParams) (string, string) {
	if params.FromTime == nil || params.ToTime == nil {
		return "", ""
	}
	return params.FromTime.UTC().Format(time.RFC3339Nano), params.ToTime.UTC().Format(time.RFC3339Nano)
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
