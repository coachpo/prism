package sidecars

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	DefaultSyncIntervalSeconds            = 300
	DefaultRequestTimeoutSeconds          = 10
	DefaultFailureThreshold               = 3
	DefaultFailureWindowSeconds           = 3600
	DefaultFallbackCooldownSeconds        = 86400
	DefaultDeprioritizedPriority          = 0
	DefaultPrioritizedPriority            = 1
	DefaultManualOverridePauseSeconds     = 1800
	DefaultProbeBatchSize                 = 3
	DefaultProbeTimeoutSeconds            = 8
	DefaultProbeBatchCooldownSeconds      = 30
	DefaultRollingRefreshAfterSeconds     = 3600
	WatchdogProbeObservationRetentionDays = 15
	ManagementAuthStateUnknown            = "unknown"
	ManagementAuthStateValid              = "valid"
	ManagementAuthStateInvalid            = "invalid_management_auth"
)

const encryptedSecretPrefix = "enc:"

type StoreErrorCode string

const (
	StoreErrorDuplicateSidecarName         StoreErrorCode = "duplicate_sidecar_name"
	StoreErrorDuplicateSidecarCanonicalURL StoreErrorCode = "duplicate_sidecar_canonical_url"
	StoreErrorDuplicateActiveHold          StoreErrorCode = "duplicate_active_hold"
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
	NextRetryAfter     *time.Time
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
	NextRetryAfter     *time.Time
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

type SidecarWatchdogPolicyInput struct {
	SidecarID                  int
	Enabled                    bool
	FailureThreshold           int
	FailureWindowSeconds       int
	FallbackCooldownSeconds    int
	DeprioritizedPriority      int
	PrioritizedPriority        int
	ManualOverridePauseSeconds int
	ProbeBatchSize             int
	ProbeTimeoutSeconds        int
	ProbeBatchCooldownSeconds  *int
	QuotaInventoryEnabled      *bool
	InitialScanEnabled         *bool
	RollingRefreshEnabled      *bool
	RollingRefreshAfterSeconds *int
}

type SidecarWatchdogPolicy struct {
	ID                         int
	SidecarID                  int
	Enabled                    bool
	FailureThreshold           int
	FailureWindowSeconds       int
	FallbackCooldownSeconds    int
	DeprioritizedPriority      int
	PrioritizedPriority        int
	ManualOverridePauseSeconds int
	ProbeBatchSize             int
	ProbeTimeoutSeconds        int
	ProbeBatchCooldownSeconds  int
	QuotaInventoryEnabled      bool
	InitialScanEnabled         bool
	RollingRefreshEnabled      bool
	RollingRefreshAfterSeconds int
	ProbeLastBatchCompletedAt  *time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type SidecarWatchdogProbeObservationInput struct {
	SidecarID          int
	AuthID             string
	AuthIndex          *string
	Provider           *string
	ProbedAt           time.Time
	ProbeStatus        string
	UpstreamStatusCode *int
	QuotaExceeded      bool
	QuotaReason        *string
	QuotaResetAt       *time.Time
	BlockingWindow     *string
	WindowsJSON        json.RawMessage
	ErrorCode          *string
}

type SidecarWatchdogProbeObservation struct {
	ID                 int
	SidecarID          int
	AuthID             string
	AuthIndex          *string
	Provider           *string
	ProbedAt           time.Time
	ProbeStatus        string
	UpstreamStatusCode *int
	QuotaExceeded      bool
	QuotaReason        *string
	QuotaResetAt       *time.Time
	BlockingWindow     *string
	WindowsJSON        json.RawMessage
	ErrorCode          *string
	CreatedAt          time.Time
}

type SidecarWatchdogProbeHoldUpdate struct {
	ID    int
	Input SidecarWatchdogHoldInput
}

type SidecarQuotaPersistDecision struct {
	SidecarID     int
	Observations  []SidecarWatchdogProbeObservationInput
	QuotaStates   []SidecarAuthQuotaStateInput
	CreateHold    *SidecarWatchdogHoldInput
	UpdateHold    *SidecarWatchdogProbeHoldUpdate
	AdvanceCursor bool
	CursorAuthID  *string
	ScanRunID     *int
}

type SidecarQuotaPersistResult struct {
	Observations   []SidecarWatchdogProbeObservation
	CreatedHold    *SidecarWatchdogHold
	UpdatedHold    *SidecarWatchdogHold
	PendingActions []SidecarWatchdogPendingAction
	ScanRun        *SidecarQuotaScanRun
	QuotaStates    []SidecarAuthQuotaState
	Policy         *SidecarWatchdogPolicy
}

type SidecarWatchdogProbeDecision = SidecarQuotaPersistDecision

type SidecarWatchdogProbeDecisionResult = SidecarQuotaPersistResult

type SidecarWatchdogHoldInput struct {
	SidecarID        int
	AuthID           string
	AuthIndex        *string
	Provider         *string
	Reason           string
	ConditionHash    string
	PreviousPriority *int
	TargetPriority   int
	HoldUntil        *time.Time
	ManualPauseUntil *time.Time
	Status           string
	LastActionID     *int
	ReleasedAt       *time.Time
}

type SidecarWatchdogHold struct {
	ID               int
	SidecarID        int
	AuthID           string
	AuthIndex        *string
	Provider         *string
	Reason           string
	ConditionHash    string
	PreviousPriority *int
	TargetPriority   int
	HoldUntil        *time.Time
	ManualPauseUntil *time.Time
	Status           string
	LastActionID     *int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ReleasedAt       *time.Time
}

type SidecarWatchdogActionInput struct {
	SidecarID        int
	AuthSnapshotID   *int
	HoldID           *int
	AuthID           *string
	AuthName         *string
	AuthIndex        *string
	Provider         *string
	ActionType       string
	Reason           *string
	PreviousPriority *int
	TargetPriority   *int
	HoldUntil        *time.Time
	Status           string
	ErrorMessage     *string
	CompletedAt      *time.Time
}

type SidecarWatchdogAction struct {
	ID               int
	SidecarID        int
	AuthSnapshotID   *int
	HoldID           *int
	AuthID           *string
	AuthName         *string
	AuthIndex        *string
	Provider         *string
	ActionType       string
	Reason           *string
	PreviousPriority *int
	TargetPriority   *int
	HoldUntil        *time.Time
	Status           string
	ErrorMessage     *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
}

type SidecarWatchdogPendingActionInput struct {
	SidecarID              int
	HoldID                 *int
	ActionHistoryCreatedAt time.Time
	ActionHistoryID        int
	AuthID                 *string
	AuthName               *string
	AuthIndex              *string
	Provider               *string
	ActionType             string
	Reason                 *string
	PreviousPriority       *int
	TargetPriority         *int
	HoldUntil              *time.Time
	AttemptCount           int
	LastAttemptAt          *time.Time
	LastErrorMessage       *string
}

type SidecarWatchdogPendingAction struct {
	ID                     int
	SidecarID              int
	HoldID                 *int
	ActionHistoryCreatedAt time.Time
	ActionHistoryID        int
	AuthID                 *string
	AuthName               *string
	AuthIndex              *string
	Provider               *string
	ActionType             string
	Reason                 *string
	PreviousPriority       *int
	TargetPriority         *int
	HoldUntil              *time.Time
	AttemptCount           int
	LastAttemptAt          *time.Time
	LastErrorMessage       *string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type SidecarQuotaScanRunInput struct {
	SidecarID          int
	ScanType           string
	Status             string
	RequestedBy        *string
	CursorAuthID       *string
	PlannedCount       int
	AttemptedCount     int
	SucceededCount     int
	QuotaExceededCount int
	FailedCount        int
	UnsupportedCount   int
	MissingIndexCount  int
	CancelRequestedAt  *time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
	LastErrorCode      *string
}

type SidecarQuotaScanRun struct {
	ID                 int
	SidecarID          int
	ScanType           string
	Status             string
	RequestedBy        *string
	CursorAuthID       *string
	PlannedCount       int
	AttemptedCount     int
	SucceededCount     int
	QuotaExceededCount int
	FailedCount        int
	UnsupportedCount   int
	MissingIndexCount  int
	CancelRequestedAt  *time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
	LastErrorCode      *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type SidecarAuthQuotaStateInput struct {
	SidecarID          int
	AuthID             string
	AuthIndex          *string
	AuthName           *string
	Provider           *string
	SnapshotObservedAt *time.Time
	State              string
	ProbeStatus        *string
	QuotaExceeded      bool
	QuotaReason        *string
	QuotaResetAt       *time.Time
	BlockingWindow     *string
	LastObservationID  *int
	LastProbedAt       *time.Time
	LastErrorCode      *string
}

type SidecarAuthQuotaState struct {
	SidecarID          int
	AuthID             string
	AuthIndex          *string
	AuthName           *string
	Provider           *string
	SnapshotObservedAt *time.Time
	State              string
	ProbeStatus        *string
	QuotaExceeded      bool
	QuotaReason        *string
	QuotaResetAt       *time.Time
	BlockingWindow     *string
	LastObservationID  *int
	LastProbedAt       *time.Time
	LastErrorCode      *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
