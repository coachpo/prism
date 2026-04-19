package profiles

import (
	"bytes"
	"encoding/json"
	"time"

	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type profileResponse struct {
	ID          int        `json:"id"`
	Name        string     `json:"name"`
	Description *string    `json:"description"`
	IsActive    bool       `json:"is_active"`
	IsDefault   bool       `json:"is_default"`
	IsEditable  bool       `json:"is_editable"`
	Version     int        `json:"version"`
	DeletedAt   *time.Time `json:"deleted_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type profileLimitsResponse struct {
	MaxProfiles int `json:"max_profiles"`
}

type profileBootstrapResponse struct {
	Profiles      []profileResponse     `json:"profiles"`
	ActiveProfile *profileResponse      `json:"active_profile"`
	ProfileLimits profileLimitsResponse `json:"profile_limits"`
}

type profileCreateRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
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

type profileUpdateRequest struct {
	Name        optionalString `json:"name"`
	Description optionalString `json:"description"`
}

type profileActivateRequest struct {
	ExpectedActiveProfileID int `json:"expected_active_profile_id"`
}

type deletedResponse struct {
	Deleted bool `json:"deleted"`
}

func profileResponseFromDomain(profile profiledomain.Profile) profileResponse {
	return profileResponse{
		ID:          profile.ID,
		Name:        profile.Name,
		Description: profile.Description,
		IsActive:    profile.IsActive,
		IsDefault:   profile.IsDefault,
		IsEditable:  profile.IsEditable,
		Version:     profile.Version,
		DeletedAt:   profile.DeletedAt,
		CreatedAt:   profile.CreatedAt,
		UpdatedAt:   profile.UpdatedAt,
	}
}
