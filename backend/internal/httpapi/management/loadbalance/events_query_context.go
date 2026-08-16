package loadbalance

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Events query context: a short-lived signed token that freezes the event
// window, profile and issue/expiry. List and detail MUST validate the context
// before reading events; a context is the events-domain slice of the
// Observe-owned query-context contract.

const eventsQueryContextVersion = 1

const (
	EventsPreset1h     = "1h"
	EventsPreset6h     = "6h"
	EventsPreset24h    = "24h"
	EventsPreset7d     = "7d"
	EventsPreset30d    = "30d"
	EventsPresetAll    = "all"
	EventsPresetCustom = "custom"
)

type eventsQueryContextPayload struct {
	Version         int        `json:"version"`
	ProfileID       int        `json:"profile_id"`
	RequestedPreset string     `json:"requested_preset"`
	FromTime        *time.Time `json:"from_time"`
	ToTime          *time.Time `json:"to_time"`
	RetentionEpoch  string     `json:"retention_epoch"`
	IssuedAt        time.Time  `json:"issued_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
}

// eventsQueryContextCodec signs and verifies context tokens with an HMAC
// subkey derived from the instance secret (no new env vars, no new tables).
type eventsQueryContextCodec struct {
	secret []byte
	now    func() time.Time
}

func newEventsQueryContextCodec(secretEncryptionKey string, now func() time.Time) *eventsQueryContextCodec {
	key := hmac.New(sha256.New, []byte(strings.TrimSpace(secretEncryptionKey)))
	_, _ = key.Write([]byte("prism.events.query-context.v1"))
	return &eventsQueryContextCodec{secret: key.Sum(nil), now: now}
}

func (codec *eventsQueryContextCodec) issue(profileID int, requestedPreset string, fromTime *time.Time, toTime *time.Time, retentionEpoch string, expiresIn time.Duration) (string, error) {
	nowAt := codec.now().UTC()
	payload := eventsQueryContextPayload{
		Version:         eventsQueryContextVersion,
		ProfileID:       profileID,
		RequestedPreset: requestedPreset,
		FromTime:        fromTime,
		ToTime:          toTime,
		RetentionEpoch:  retentionEpoch,
		IssuedAt:        nowAt,
		ExpiresAt:       nowAt.Add(expiresIn),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal events query context: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	signature := codec.sign(encoded)
	return encoded + "." + signature, nil
}

func (codec *eventsQueryContextCodec) sign(payload string) string {
	mac := hmac.New(sha256.New, codec.secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// validate returns the decoded context for the given profile. Invalid tokens
// map to the typed error codes the frontend uses to rebuild the context.
func (codec *eventsQueryContextCodec) validate(raw string, profileID int) (eventsQueryContextPayload, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), ".", 2)
	if len(parts) != 2 {
		return eventsQueryContextPayload{}, &domainError{StatusCode: 422, Code: "invalid_query_context", Detail: "invalid query context"}
	}
	expected := codec.sign(parts[0])
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return eventsQueryContextPayload{}, &domainError{StatusCode: 422, Code: "invalid_query_context", Detail: "invalid query context"}
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return eventsQueryContextPayload{}, &domainError{StatusCode: 422, Code: "invalid_query_context", Detail: "invalid query context"}
	}
	var payload eventsQueryContextPayload
	if err := json.Unmarshal(decoded, &payload); err != nil || payload.Version != eventsQueryContextVersion {
		return eventsQueryContextPayload{}, &domainError{StatusCode: 422, Code: "invalid_query_context", Detail: "invalid query context"}
	}
	if payload.ProfileID != profileID {
		// Cross-profile context: never leak existence.
		return eventsQueryContextPayload{}, &domainError{StatusCode: 404, Detail: "Events query context not found"}
	}
	if !codec.now().UTC().Before(payload.ExpiresAt) {
		return eventsQueryContextPayload{}, &domainError{StatusCode: 410, Code: "query_context_expired", Detail: "query context expired; rebuild the event window"}
	}
	return payload, nil
}
