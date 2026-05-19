package sidecars

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	DefaultSyncIntervalSeconds   = 300
	DefaultRequestTimeoutSeconds = 10
	ManagementAuthStateUnknown   = "unknown"
	ManagementAuthStateValid     = "valid"
	ManagementAuthStateInvalid   = "invalid_management_auth"
)

const encryptedSecretPrefix = "enc:"

type StoreErrorCode string

const (
	StoreErrorDuplicateSidecarName         StoreErrorCode = "duplicate_sidecar_name"
	StoreErrorDuplicateSidecarCanonicalURL StoreErrorCode = "duplicate_sidecar_canonical_url"
	StoreErrorConflict                     StoreErrorCode = "conflict"
	StoreErrorNotFound                     StoreErrorCode = "not_found"
	StoreErrorInvalidInput                 StoreErrorCode = "invalid_input"
)

type StoreError struct {
	Code    StoreErrorCode
	Message string
	Err     error
}

func (err *StoreError) Error() string {
	if err == nil {
		return ""
	}
	if err.Message != "" {
		return err.Message
	}
	return string(err.Code)
}

func (err *StoreError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func IsStoreError(err error, code StoreErrorCode) bool {
	var storeErr *StoreError
	return errors.As(err, &storeErr) && storeErr.Code == code
}

type SidecarInstanceInput struct {
	Name                          string
	BaseURL                       string
	BaseURLCanonical              string
	ManagementPassword            string
	ManagementPasswordIsEncrypted bool
	Enabled                       *bool
	EnvironmentLabel              *string
	SyncIntervalSeconds           int
	RequestTimeoutSeconds         int
	AllowPrivateNetwork           bool
	AllowInsecureHTTP             bool
	SkipTLSVerify                 bool
	ManagementAuthState           string
	AuthFailurePauseUntil         *time.Time
}

type SidecarInstance struct {
	ID                          int
	Name                        string
	BaseURL                     string
	BaseURLCanonical            string
	EncryptedManagementPassword string
	Enabled                     bool
	EnvironmentLabel            *string
	SyncIntervalSeconds         int
	RequestTimeoutSeconds       int
	AllowPrivateNetwork         bool
	AllowInsecureHTTP           bool
	SkipTLSVerify               bool
	LastSyncAt                  *time.Time
	LastSuccessfulSyncAt        *time.Time
	SnapshotStaleAfter          *time.Time
	LastSyncError               *string
	ManagementAuthState         string
	AuthFailurePauseUntil       *time.Time
	DeletedAt                   *time.Time
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

type SidecarSyncMetadataInput struct {
	SidecarID             int
	LastSyncAt            time.Time
	LastSuccessfulSyncAt  *time.Time
	SnapshotStaleAfter    *time.Time
	LastSyncError         *string
	ManagementAuthState   string
	AuthFailurePauseUntil *time.Time
}

type SidecarAuthSnapshotInput struct {
	SidecarID          int
	AuthID             string
	AuthIndex          *string
	Name               string
	Provider           *string
	Label              *string
	Status             *string
	StatusMessage      *string
	Disabled           *bool
	Unavailable        *bool
	Priority           *int
	QuotaExceeded      *bool
	QuotaReason        *string
	QuotaNextRecoverAt *time.Time
	SuccessCount       *int
	FailedCount        *int
	RecentRequestsJSON json.RawMessage
	ModelStatesJSON    json.RawMessage
	SnapshotJSON       json.RawMessage
	ObservedAt         time.Time
}

type SidecarAuthSnapshot struct {
	ID                 int
	SidecarID          int
	AuthID             string
	AuthIndex          *string
	Name               string
	Provider           *string
	Label              *string
	Status             *string
	StatusMessage      *string
	Disabled           *bool
	Unavailable        *bool
	Priority           *int
	QuotaExceeded      *bool
	QuotaReason        *string
	QuotaNextRecoverAt *time.Time
	SuccessCount       *int
	FailedCount        *int
	RecentRequestsJSON json.RawMessage
	ModelStatesJSON    json.RawMessage
	SnapshotJSON       json.RawMessage
	ObservedAt         time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type SidecarProviderSnapshotInput struct {
	SidecarID       int
	ProviderKey     string
	ProviderItemKey string
	Name            *string
	Label           *string
	Status          *string
	Disabled        *bool
	SnapshotJSON    json.RawMessage
	ObservedAt      time.Time
}

type SidecarProviderSnapshotBatch struct {
	ProviderKey string
	Inputs      []SidecarProviderSnapshotInput
	Replace     bool
}

type SidecarProviderSnapshot struct {
	ID              int
	SidecarID       int
	ProviderKey     string
	ProviderItemKey string
	Name            *string
	Label           *string
	Status          *string
	Disabled        *bool
	SnapshotJSON    json.RawMessage
	ObservedAt      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
