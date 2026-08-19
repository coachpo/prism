package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
)

// Settings contracts for retention, preflight, and audit policy/storage flows
// (Settings SPEC §3/§5/§6/§9). Counts/revisions that may exceed JavaScript
// safe integer are decimal strings. `null` retention means "no logical
// retention cutoff", never "permanent raw rows".

type retentionPolicyReadValue struct {
	State string `json:"state"` // "valid" | "repair_required"
	// Value is deliberately not omitted. The repair union must distinguish an
	// explicit valid NULL from a missing field while preserving valid siblings.
	Value      *int    `json:"value"`
	RawInteger *string `json:"raw_integer,omitempty"`
	Issue      *string `json:"issue,omitempty"`
}

type retentionPolicies struct {
	RequestLogsRetentionDays       *int `json:"request_logs_retention_days"`
	AuditLogsRetentionDays         *int `json:"audit_logs_retention_days"`
	StatisticsRetentionDays        *int `json:"statistics_retention_days"`
	LoadbalanceEventsRetentionDays *int `json:"loadbalance_events_retention_days"`
}

type retentionPolicyReadPolicies struct {
	RequestLogsRetentionDays       retentionPolicyReadValue `json:"request_logs_retention_days"`
	AuditLogsRetentionDays         retentionPolicyReadValue `json:"audit_logs_retention_days"`
	StatisticsRetentionDays        retentionPolicyReadValue `json:"statistics_retention_days"`
	LoadbalanceEventsRetentionDays retentionPolicyReadValue `json:"loadbalance_events_retention_days"`
}

// UnmarshalJSON keeps missing policy fields distinct from an explicit null.
// A full replacement must carry all four fields; accepting a partially
// decoded struct would silently preserve a sibling value and violate the
// atomic four-field Settings contract.
func (policies *retentionPolicies) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&fields); err != nil {
		return err
	}
	if fields == nil {
		return fmt.Errorf("policies must be an object")
	}
	allowed := map[string]struct{}{
		"request_logs_retention_days":       {},
		"audit_logs_retention_days":         {},
		"statistics_retention_days":         {},
		"loadbalance_events_retention_days": {},
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown retention policy field %q", key)
		}
	}
	if len(fields) != len(allowed) {
		return fmt.Errorf("all four retention policy fields are required")
	}
	decode := func(name string) (*int, error) {
		var value *int
		if err := json.Unmarshal(fields[name], &value); err != nil {
			return nil, err
		}
		return value, nil
	}
	var err error
	if policies.RequestLogsRetentionDays, err = decode("request_logs_retention_days"); err != nil {
		return err
	}
	if policies.AuditLogsRetentionDays, err = decode("audit_logs_retention_days"); err != nil {
		return err
	}
	if policies.StatisticsRetentionDays, err = decode("statistics_retention_days"); err != nil {
		return err
	}
	if policies.LoadbalanceEventsRetentionDays, err = decode("loadbalance_events_retention_days"); err != nil {
		return err
	}
	return nil
}

type retentionRecommendation struct {
	ID             string            `json:"id"`
	Policies       retentionPolicies `json:"policies"`
	RationaleCodes []string          `json:"rationale_codes"`
}

type observeTokenProtection struct {
	Kind                     string  `json:"kind"`
	TokenTTLSeconds          int64   `json:"token_ttl_seconds"`
	ExtraGraceSeconds        int64   `json:"extra_grace_seconds"`
	PhysicalReclaimNotBefore *string `json:"physical_reclaim_not_before"`
	SourceRevision           string  `json:"source_revision"`
	RetentionEpoch           string  `json:"retention_epoch"`
	RetentionGeneration      string  `json:"retention_generation"`
	PurgeState               string  `json:"purge_state"`
}

type auditFenceProtection struct {
	Kind                     string                                       `json:"kind"`
	ContractVersion          int                                          `json:"contract_version"`
	RetentionSource          any                                          `json:"retention_source"`
	AuditProtection          auditdomain.AuditFenceMaterializerProjection `json:"audit_protection"`
	FixedTokenTTLSeconds     *int                                         `json:"fixed_token_ttl_seconds"`
	FixedExtraGraceSeconds   *int                                         `json:"fixed_extra_grace_seconds"`
	PhysicalReclaimNotBefore *string                                      `json:"physical_reclaim_not_before"`
}

type retentionCoverageSummary struct {
	FromTime            *string `json:"from_time"`
	ToTime              *string `json:"to_time"`
	CoverageRevision    string  `json:"coverage_revision"`
	CoverageHash        string  `json:"coverage_hash"`
	GeneratedAt         *string `json:"generated_at"`
	Source              string  `json:"source"`
	Precision           string  `json:"precision"`
	Gaps                []any   `json:"gaps"`
	Complete            bool    `json:"complete"`
	Freshness           string  `json:"freshness"`
	SourceRevision      string  `json:"source_revision"`
	RetentionEpoch      string  `json:"retention_epoch"`
	RetentionGeneration string  `json:"retention_generation"`
	PurgeState          string  `json:"purge_state"`
	ErrorCode           *string `json:"error_code,omitempty"`
}

type retentionOwnerDriftHead struct {
	HeadID            string                   `json:"head_id"`
	LineageGeneration string                   `json:"lineage_generation"`
	PredecessorHeadID *string                  `json:"predecessor_head_id"`
	Field             string                   `json:"field"`
	EvidenceHash      string                   `json:"evidence_hash"`
	InstanceValue     retentionPolicyReadValue `json:"instance_value"`
	LegacyCopyValue   retentionPolicyReadValue `json:"legacy_copy_value"`
	ResolutionState   string                   `json:"resolution_state"` // drift | converged | archived
	Resolution        string                   `json:"resolution"`
	GeneratedAt       string                   `json:"generated_at"`
	ResolvedAt        *string                  `json:"resolved_at"`
}

type retentionOwnerDriftInventory struct {
	InventoryGeneration string                    `json:"inventory_generation"`
	State               string                    `json:"state"` // action_required | resolved
	CurrentHeads        []retentionOwnerDriftHead `json:"current_heads"`
	GeneratedAt         string                    `json:"generated_at"`
}

type logRetentionSettingsResponse struct {
	State     string `json:"state"` // ready | repair_required
	Scope     string `json:"scope"`
	Revision  string `json:"revision"`
	UpdatedAt string `json:"updated_at"`
	ServerNow string `json:"server_now"`
	// Ready responses expose the editable raw four-field replacement. Repair
	// responses expose the tagged union so a legacy out-of-range integer is
	// never coerced to null and valid siblings remain round-trippable.
	Policies                 any                                 `json:"policies"`
	Recommendations          []retentionRecommendation           `json:"recommendations"`
	PolicyGeneration         map[string]string                   `json:"policy_generation"`
	ConfiguredLogicalCutoffs map[string]*string                  `json:"configured_logical_cutoffs"`
	PublishedRetentionFloors map[string]*string                  `json:"published_retention_floors"`
	RetentionSourceRevision  map[string]string                   `json:"retention_source_revision"`
	ActualCoverage           map[string]retentionCoverageSummary `json:"actual_coverage"`
	Protection               map[string]any                      `json:"protection"`
	OwnerDriftInventory      *retentionOwnerDriftInventory       `json:"owner_drift_inventory,omitempty"`
	RepairPreflightURL       string                              `json:"repair_preflight_url,omitempty"`
}

type archiveRetentionOwnerDriftRequest struct {
	OperationID                 string `json:"operation_id"`
	ExpectedRevision            string `json:"expected_revision"`
	ExpectedInventoryGeneration string `json:"expected_inventory_generation"`
	Heads                       []struct {
		Field        string `json:"field"`
		HeadID       string `json:"head_id"`
		EvidenceHash string `json:"evidence_hash"`
	} `json:"heads"`
	Acknowledgement string `json:"acknowledgement"`
}

type putLogRetentionSettingsRequest struct {
	OperationID      string            `json:"operation_id"`
	ExpectedRevision string            `json:"expected_revision"`
	Policies         retentionPolicies `json:"policies"`
	PreflightToken   *string           `json:"preflight_token,omitempty"`
	Confirmation     *struct {
		Keyword string `json:"keyword"`
	} `json:"confirmation,omitempty"`
}

type retentionChangeItem struct {
	Dataset            string                   `json:"dataset"`
	Before             retentionPolicyReadValue `json:"before"`
	AfterDays          *int                     `json:"after_days"`
	Destructive        bool                     `json:"destructive"`
	LogicalCutoff      *string                  `json:"logical_cutoff"`
	ProtectionDeadline *string                  `json:"protection_deadline"`
}

type retentionScheduledWork struct {
	Dataset          string `json:"dataset"`
	PolicyGeneration string `json:"policy_generation"`
	Disposition      string `json:"disposition"` // created | woken | waiting_for_resource
	JobID            string `json:"job_id"`
}

type putLogRetentionSettingsResult struct {
	Settings      logRetentionSettingsResponse `json:"settings"`
	Changes       []retentionChangeItem        `json:"changes"`
	ScheduledWork []retentionScheduledWork     `json:"scheduled_work"`
	OperationID   string                       `json:"operation_id"`
	Replayed      bool                         `json:"replayed"`
}

// ---- preflight (SPEC §6) ----

// These request contracts remain named because the live preflight handlers
// decode them as part of the destructive-retention boundary.
type policyChangePreflightRequest struct {
	Kind                     string            `json:"kind"`
	OperationID              string            `json:"operation_id"`
	PreflightAttemptID       string            `json:"preflight_attempt_id"`
	ExpectedSettingsRevision string            `json:"expected_settings_revision"`
	Policies                 retentionPolicies `json:"policies"`
}

type manualCleanupSelection struct {
	Mode   string  `json:"mode"` // keep_days | cutoff | delete_all
	Days   *int    `json:"days,omitempty"`
	Cutoff *string `json:"cutoff,omitempty"`
}

// UnmarshalJSON keeps the manual cleanup selection a closed discriminated
// union. In particular, decoding into the struct directly would silently
// accept an unrelated sibling (for example {mode:"delete_all", days:7}) and
// make the confirmation text describe a different operation than the server
// executes.
func (selection *manualCleanupSelection) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&fields); err != nil {
		return err
	}
	if fields == nil {
		return fmt.Errorf("selection must be an object")
	}
	for key := range fields {
		switch key {
		case "mode", "days", "cutoff":
		default:
			return fmt.Errorf("unknown cleanup selection field %q", key)
		}
	}
	modeValue, ok := fields["mode"]
	if !ok {
		return fmt.Errorf("selection.mode is required")
	}
	var mode string
	if err := json.Unmarshal(modeValue, &mode); err != nil || strings.TrimSpace(mode) == "" {
		if err != nil {
			return fmt.Errorf("selection.mode must be a string: %w", err)
		}
		return fmt.Errorf("selection.mode is required")
	}
	var days *int
	if raw, ok := fields["days"]; ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("selection.days must be an integer when present")
		}
		if err := json.Unmarshal(raw, &days); err != nil || days == nil {
			if err != nil {
				return fmt.Errorf("selection.days must be an integer: %w", err)
			}
			return fmt.Errorf("selection.days must be an integer when present")
		}
	}
	var cutoff *string
	if raw, ok := fields["cutoff"]; ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("selection.cutoff must be a timestamp when present")
		}
		if err := json.Unmarshal(raw, &cutoff); err != nil || cutoff == nil || strings.TrimSpace(*cutoff) == "" {
			if err != nil {
				return fmt.Errorf("selection.cutoff must be a timestamp: %w", err)
			}
			return fmt.Errorf("selection.cutoff must be a timestamp when present")
		}
	}
	switch mode {
	case "keep_days":
		if days == nil || cutoff != nil {
			return fmt.Errorf("keep_days requires days and forbids cutoff")
		}
	case "cutoff":
		if cutoff == nil || days != nil {
			return fmt.Errorf("cutoff requires cutoff and forbids days")
		}
	case "delete_all":
		if days != nil || cutoff != nil {
			return fmt.Errorf("delete_all forbids days and cutoff")
		}
	default:
		return fmt.Errorf("unsupported cleanup selection mode %q", mode)
	}
	selection.Mode = mode
	selection.Days = days
	selection.Cutoff = cutoff
	return nil
}

type manualCleanupPreflightRequest struct {
	Kind               string                 `json:"kind"`
	OperationID        string                 `json:"operation_id"`
	PreflightAttemptID string                 `json:"preflight_attempt_id"`
	Dataset            string                 `json:"dataset"`
	Selection          manualCleanupSelection `json:"selection"`
}

type retentionImpactCount struct {
	Value    *string `json:"value"`
	Accuracy string  `json:"accuracy"` // exact | estimated | unavailable
	Method   string  `json:"method"`
}

type retentionImpactBytes struct {
	Value    *string `json:"value"`
	Accuracy string  `json:"accuracy"`
	Basis    string  `json:"basis"`
}

type retentionCoverageProjection struct {
	FromTime *string `json:"from_time"`
	ToTime   string  `json:"to_time"`
	Gaps     []any   `json:"gaps"`
	Accuracy string  `json:"accuracy"`
	Basis    string  `json:"basis"`
	// CoverageReady is an internal gate only. The wire projection deliberately
	// keeps accuracy/basis separate so delete-all never presents a preview as an
	// exact empty range while a fresh owner model may still prove its semantic
	// source, fence and non-cascade facts.
	CoverageReady bool `json:"-"`
}

type retentionImpactDetails struct {
	Change                   any                         `json:"change"`
	ResolvedCutoff           *string                     `json:"resolved_cutoff"`
	LogicalCoverageAfter     retentionCoverageProjection `json:"logical_coverage_after"`
	PhysicalReclaimNotBefore *string                     `json:"physical_reclaim_not_before"`
	MatchedRows              retentionImpactCount        `json:"matched_rows"`
	RetainedRows             retentionImpactCount        `json:"retained_rows"`
	MatchedLogicalBytes      retentionImpactBytes        `json:"matched_logical_bytes"`
	ReclaimablePhysicalBytes retentionImpactBytes        `json:"reclaimable_physical_bytes"`
	MatchedFraction          *string                     `json:"matched_fraction"`
	WholePartitions          any                         `json:"whole_partitions"`
	BoundaryPartitions       []any                       `json:"boundary_partitions"`
	StorageLayers            []any                       `json:"storage_layers"`
	Consumers                []string                    `json:"consumers"`
	NonCascades              []any                       `json:"non_cascades"`
	SemanticFactsComplete    bool                        `json:"semantic_facts_complete"`
	Warnings                 []string                    `json:"warnings"`
}

type retentionAffectedDomain struct {
	Dataset       string                 `json:"dataset"`
	OwnerSnapshot any                    `json:"owner_snapshot"`
	Impact        retentionImpactDetails `json:"impact"`
}

type retentionPreflightResponse struct {
	PreflightID         string                    `json:"preflight_id"`
	PreflightToken      string                    `json:"preflight_token"`
	Kind                string                    `json:"kind"`
	OperationID         string                    `json:"operation_id"`
	PreflightAttemptID  string                    `json:"preflight_attempt_id"`
	Scope               string                    `json:"scope"`
	RequestHash         string                    `json:"request_hash"`
	PreviewedAt         string                    `json:"previewed_at"`
	GeneratedAt         string                    `json:"generated_at"`
	ExpiresAt           string                    `json:"expires_at"`
	SettingsRevision    string                    `json:"settings_revision"`
	AffectedDomains     []retentionAffectedDomain `json:"affected_domains"`
	ConfirmationKeyword string                    `json:"confirmation_keyword"`
}

type createManualRetentionJobRequest struct {
	OperationID    string `json:"operation_id"`
	PreflightToken string `json:"preflight_token"`
	Confirmation   struct {
		Keyword string `json:"keyword"`
	} `json:"confirmation"`
}

// ---- audit settings (SPEC §9) ----

type auditPolicyRow struct {
	Family string `json:"family"`
	Mode   string `json:"mode"` // disabled | metadata_only | body_capture
}

type targetAuditSettingsResponse struct {
	Revision           string           `json:"revision"`
	UpdatedAt          string           `json:"updated_at"`
	Policies           []auditPolicyRow `json:"policies"`
	FixedCaptureLimits map[string]int64 `json:"fixed_capture_limits"`
}

type putAuditSettingsRequest struct {
	OperationID      string           `json:"operation_id"`
	ExpectedRevision string           `json:"expected_revision"`
	Policies         []auditPolicyRow `json:"policies"`
}

type putAuditSettingsResponse struct {
	OperationID string                      `json:"operation_id"`
	Replayed    bool                        `json:"replayed"`
	Settings    targetAuditSettingsResponse `json:"settings"`
}

type auditStorageSummary struct {
	SourceRevision           string  `json:"source_revision"`
	StorageFactEvidence      any     `json:"storage_fact_evidence"`
	GeneratedAt              string  `json:"generated_at"`
	RetentionSource          any     `json:"retention_source"`
	AuditProtection          any     `json:"audit_protection"`
	RetainedRows             *string `json:"retained_rows"`
	LogicalHeaderBytes       *string `json:"logical_header_bytes"`
	LogicalBodyBytes         *string `json:"logical_body_bytes"`
	Last7dLogicalBytesAdded  *string `json:"last_7d_logical_bytes_added"`
	SampledDays              int     `json:"sampled_days"`
	DailyAverageLogicalBytes *string `json:"daily_average_logical_bytes"`
	Precision                string  `json:"precision"`
	Freshness                string  `json:"freshness"`
}

// Canonical constants shared by handlers and golden tests.
const (
	retentionDatasetRequestLogs        = "request_logs"
	retentionDatasetAuditLogs          = "audit_logs"
	retentionDatasetUsageRequestEvents = "usage_request_events"
	retentionDatasetLoadbalanceEvents  = "loadbalance_events"

	retentionMaxDays = 36500
)

var retentionDatasets = []string{
	retentionDatasetRequestLogs,
	retentionDatasetAuditLogs,
	retentionDatasetUsageRequestEvents,
	retentionDatasetLoadbalanceEvents,
}

// balanced-v1 recommendation (SPEC §5.1): fills the client draft only, never
// mutates on page load/seed/migration and never bypasses the destructive
// classifier.
func balancedV1Recommendation() retentionRecommendation {
	return retentionRecommendation{
		ID: "balanced-v1",
		Policies: retentionPolicies{
			RequestLogsRetentionDays:       intPtr(30),
			AuditLogsRetentionDays:         intPtr(7),
			StatisticsRetentionDays:        intPtr(90),
			LoadbalanceEventsRetentionDays: intPtr(30),
		},
		RationaleCodes: []string{"investigation_window", "statistics_window", "audit_sensitivity", "storage_growth"},
	}
}
