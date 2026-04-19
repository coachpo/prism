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
}

type endpointPositionMoveRequest struct {
	ToIndex int `json:"to_index"`
}

type endpointResponse struct {
	ID           int       `json:"id"`
	ProfileID    int       `json:"profile_id"`
	Name         string    `json:"name"`
	BaseURL      string    `json:"base_url"`
	HasAPIKey    bool      `json:"has_api_key"`
	MaskedAPIKey *string   `json:"masked_api_key"`
	Position     int       `json:"position"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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

type endpointUsageConnection struct {
	ConnectionID  int
	ModelConfigID int
	ModelID       *string
	Name          *string
}
