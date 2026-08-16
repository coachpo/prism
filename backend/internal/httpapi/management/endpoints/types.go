package endpoints

import (
	"bytes"
	"encoding/json"
	"time"
)

type optionalString struct {
	Set   bool
	Value *string
}

func (value *optionalString) UnmarshalJSON(data []byte) error {
	value.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed string
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	value.Value = &parsed
	return nil
}

type endpointCreateRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type endpointUpdateRequest struct {
	Name    optionalString `json:"name"`
	BaseURL optionalString `json:"base_url"`
	APIKey  optionalString `json:"api_key"`
	// ExpectedUpdatedAt is the optimistic-concurrency guard, mirroring the
	// pricing-template expected_updated_at contract (architecture.md:1145).
	// RFC3339; a mismatch against the stored row returns 409 endpoint_stale.
	ExpectedUpdatedAt *string `json:"expected_updated_at"`
}

type endpointStaleDetail struct {
	Code     string           `json:"code"` // "endpoint_stale"
	Message  string           `json:"message"`
	Endpoint endpointResponse `json:"endpoint"` // current server state for the frontend to refresh
}

// endpointResponse is the shared Endpoint read contract. Raw secrets and the
// legacy masked constant never appear here; key identity is exposed as a
// deterministic display fingerprint plus an independent key-identity time.
type endpointResponse struct {
	ID                int        `json:"id"`
	ProfileID         int        `json:"profile_id"`
	Name              string     `json:"name"`
	BaseURL           string     `json:"base_url"`
	HasAPIKey         bool       `json:"has_api_key"`
	APIKeyFingerprint *string    `json:"api_key_fingerprint"`
	APIKeyUpdatedAt   *time.Time `json:"api_key_updated_at"`
	ConfigRevision    int64      `json:"config_revision"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type connectionDropdownItem struct {
	ID         int     `json:"id"`
	EndpointID int     `json:"endpoint_id"`
	Name       *string `json:"name"`
}

type connectionDropdownResponse struct {
	Items []connectionDropdownItem `json:"items"`
}

type deletedResponse struct {
	Deleted bool `json:"deleted"`
}
