package configrules

import (
	"bytes"
	"encoding/json"
	"time"
)

type headerBlocklistRuleResponse struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	MatchType string    `json:"match_type"`
	Pattern   string    `json:"pattern"`
	Enabled   bool      `json:"enabled"`
	IsSystem  bool      `json:"is_system"`
	ProfileID *int      `json:"profile_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type headerBlocklistRuleCreateRequest struct {
	Name      string `json:"name"`
	MatchType string `json:"match_type"`
	Pattern   string `json:"pattern"`
	Enabled   *bool  `json:"enabled"`
}

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

type optionalBool struct {
	Set   bool
	Value bool
}

func (value *optionalBool) UnmarshalJSON(data []byte) error {
	value.Set = true
	return json.Unmarshal(bytes.TrimSpace(data), &value.Value)
}

type headerBlocklistRuleUpdateRequest struct {
	Name      optionalString `json:"name"`
	MatchType optionalString `json:"match_type"`
	Pattern   optionalString `json:"pattern"`
	Enabled   optionalBool   `json:"enabled"`
}

type userAgentClientRuleResponse struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Pattern   string    `json:"pattern"`
	Enabled   bool      `json:"enabled"`
	IsSystem  bool      `json:"is_system"`
	ProfileID *int      `json:"profile_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type userAgentClientRuleCreateRequest struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Enabled *bool  `json:"enabled"`
}

type userAgentClientRuleUpdateRequest struct {
	Name    optionalString `json:"name"`
	Pattern optionalString `json:"pattern"`
	Enabled optionalBool   `json:"enabled"`
}

type deletedResponse struct {
	Deleted bool `json:"deleted"`
}

type headerBlocklistRuleRow struct {
	ID        int
	Name      string
	MatchType string
	Pattern   string
	Enabled   bool
	IsSystem  bool
	ProfileID *int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type userAgentClientRuleRow struct {
	ID        int
	Name      string
	Pattern   string
	Enabled   bool
	IsSystem  bool
	ProfileID *int
	CreatedAt time.Time
	UpdatedAt time.Time
}
