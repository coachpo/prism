package managementjobs

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Keyset cursors are opaque and HMAC-signed with the store's derived signing
// key: a client can neither read the raw keyset fields nor move the position
// without invalidating the signature.

const (
	jobsCursorSort     = "requested_at_desc_id_desc"
	evidenceCursorSort = "sequence_asc"
)

type jobsCursorPayload struct {
	Version    int      `json:"v"`
	Origin     string   `json:"origin,omitempty"`
	States     []string `json:"states,omitempty"`
	Limit      int      `json:"limit"`
	Sort       string   `json:"sort"`
	UpperAt    string   `json:"upper_at"`
	UpperID    string   `json:"upper_id"`
	PositionAt string   `json:"position_at"`
	PositionID string   `json:"position_id"`
	Signature  string   `json:"sig"`
}

type evidenceCursorPayload struct {
	Version    int    `json:"v"`
	Kind       string `json:"kind"`
	JobID      string `json:"job_id"`
	Limit      int    `json:"limit"`
	Sort       string `json:"sort"`
	UpperID    int64  `json:"upper_id"`
	PositionID int64  `json:"position_id"`
	Signature  string `json:"sig"`
}

func canonicalJobStates(states []string) []string {
	seen := make(map[string]struct{}, len(states))
	result := make([]string, 0, len(states))
	for _, state := range states {
		state = strings.TrimSpace(state)
		if state == "" {
			continue
		}
		if _, exists := seen[state]; exists {
			continue
		}
		seen[state] = struct{}{}
		result = append(result, state)
	}
	sort.Strings(result)
	return result
}

func sameJobStates(left, right []string) bool {
	left = canonicalJobStates(left)
	right = canonicalJobStates(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *Store) encodeJobsCursor(payload jobsCursorPayload) string {
	payload.Signature = ""
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write(raw)
	payload.Signature = hex.EncodeToString(mac.Sum(nil))
	encoded, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func (s *Store) decodeJobsCursor(encoded string) (jobsCursorPayload, bool) {
	var payload jobsCursorPayload
	if len(encoded) > 4096 {
		return payload, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 {
		return payload, false
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Version != 2 || payload.Limit < 1 || payload.Limit > 100 || payload.Sort != jobsCursorSort || strings.TrimSpace(payload.UpperID) == "" || strings.TrimSpace(payload.PositionID) == "" || strings.TrimSpace(payload.Signature) == "" {
		return jobsCursorPayload{}, false
	}
	provided, err := hex.DecodeString(payload.Signature)
	if err != nil || len(provided) != sha256.Size {
		return jobsCursorPayload{}, false
	}
	signature := payload.Signature
	payload.Signature = ""
	unsigned, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write(unsigned)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return jobsCursorPayload{}, false
	}
	payload.Signature = signature
	if _, err := time.Parse(time.RFC3339Nano, payload.UpperAt); err != nil {
		return jobsCursorPayload{}, false
	}
	if _, err := time.Parse(time.RFC3339Nano, payload.PositionAt); err != nil {
		return jobsCursorPayload{}, false
	}
	return payload, true
}

func (s *Store) encodeEvidenceCursor(payload evidenceCursorPayload) string {
	payload.Signature = ""
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write(raw)
	payload.Signature = hex.EncodeToString(mac.Sum(nil))
	encoded, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func (s *Store) decodeEvidenceCursor(encoded, kind, jobID string, limit int) (evidenceCursorPayload, bool) {
	var payload evidenceCursorPayload
	if len(encoded) > 4096 {
		return payload, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || json.Unmarshal(raw, &payload) != nil || payload.Version != 2 || payload.Kind != kind || payload.JobID != jobID || payload.Limit != limit || payload.Sort != evidenceCursorSort || payload.UpperID < 0 || payload.PositionID < 0 || payload.PositionID > payload.UpperID || strings.TrimSpace(payload.Signature) == "" {
		return evidenceCursorPayload{}, false
	}
	provided, err := hex.DecodeString(payload.Signature)
	if err != nil || len(provided) != sha256.Size {
		return evidenceCursorPayload{}, false
	}
	signature := payload.Signature
	payload.Signature = ""
	unsigned, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write(unsigned)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return evidenceCursorPayload{}, false
	}
	payload.Signature = signature
	return payload, true
}

// EncodeJobsCursor / DecodeJobsCursor expose the opaque keyset cursor.
func EncodeJobsCursor(requestedAt time.Time, id string) string {
	key := deriveCursorSigningKey("", "public-helper")
	store := &Store{cursorKey: key}
	stamp := requestedAt.UTC().Format(time.RFC3339Nano)
	return store.encodeJobsCursor(jobsCursorPayload{
		Version:    2,
		Limit:      20,
		Sort:       jobsCursorSort,
		UpperAt:    stamp,
		UpperID:    id,
		PositionAt: stamp,
		PositionID: id,
	})
}

func DecodeJobsCursor(encoded string) (time.Time, string, bool) {
	key := deriveCursorSigningKey("", "public-helper")
	store := &Store{cursorKey: key}
	decoded, ok := store.decodeJobsCursor(encoded)
	if !ok {
		return time.Time{}, "", false
	}
	parsed, err := time.Parse(time.RFC3339Nano, decoded.PositionAt)
	if err != nil {
		return time.Time{}, "", false
	}
	return parsed, decoded.PositionID, true
}
