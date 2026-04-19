package vendors

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/coachpo/prism/backend/internal/vendordomain"
)

type vendorResponse struct {
	ID                 int       `json:"id"`
	Key                string    `json:"key"`
	Name               string    `json:"name"`
	Description        *string   `json:"description"`
	IconKey            *string   `json:"icon_key"`
	IsReadonly         bool      `json:"is_readonly"`
	AuditEnabled       bool      `json:"audit_enabled"`
	AuditCaptureBodies bool      `json:"audit_capture_bodies"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type vendorCreateRequest struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IconKey     *string `json:"icon_key"`
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

type vendorUpdateRequest struct {
	Key                optionalString `json:"key"`
	Name               optionalString `json:"name"`
	Description        optionalString `json:"description"`
	IconKey            optionalString `json:"icon_key"`
	AuditEnabled       optionalBool   `json:"audit_enabled"`
	AuditCaptureBodies optionalBool   `json:"audit_capture_bodies"`
}

type vendorModelUsageItem struct {
	ModelConfigID int     `json:"model_config_id"`
	ProfileID     int     `json:"profile_id"`
	ProfileName   string  `json:"profile_name"`
	ModelID       string  `json:"model_id"`
	DisplayName   *string `json:"display_name"`
	ModelType     string  `json:"model_type"`
	APIFamily     string  `json:"api_family"`
	IsEnabled     bool    `json:"is_enabled"`
}

func vendorResponseFromRecord(record vendorRecord) vendorResponse {
	return vendorResponse{
		ID:                 record.ID,
		Key:                record.Key,
		Name:               record.Name,
		Description:        record.Description,
		IconKey:            record.IconKey,
		IsReadonly:         vendordomain.IsReadonlyVendorKey(record.Key),
		AuditEnabled:       record.AuditEnabled,
		AuditCaptureBodies: record.AuditCaptureBodies,
		CreatedAt:          record.CreatedAt,
		UpdatedAt:          record.UpdatedAt,
	}
}
