package settings

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

// SettingsProblem is the flat management error envelope (Settings SPEC §4.1).
// Every changed settings route emits exactly
// `{code, detail, params, details, request_id}`; recovery fields, revisions,
// field violations, retry and cutoff suggestions live only in the
// code-registered `details` payload. There is no legacy top-level recovery
// field and no nested detail object.
type SettingsProblem struct {
	Code      string         `json:"code"`
	Detail    string         `json:"detail"`
	Params    map[string]any `json:"params"`
	Details   any            `json:"details"`
	RequestID string         `json:"request_id"`
}

// settingsMutationError carries a registered HTTP problem through a database
// transaction. Handlers must not write to the response while a transaction is
// still open: a later rollback would otherwise leave the client with a false
// success or a misleading conflict.
type settingsMutationError struct {
	Problem SettingsProblem
	Status  int
}

func (err *settingsMutationError) Error() string { return err.Problem.Detail }

func newSettingsMutationError(status int, problem SettingsProblem) error {
	return &settingsMutationError{Problem: problem, Status: status}
}

func writeSettingsMutationError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) bool {
	var mutationErr *settingsMutationError
	if !errors.As(err, &mutationErr) {
		return false
	}
	writeProblem(w, r, corsSnapshot, mutationErr.Problem, mutationErr.Status)
	return true
}

// FieldViolation is the registered details payload for validation codes.
type FieldViolation struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Limit  any    `json:"limit,omitempty"`
}

// Registered details payloads (SPEC §4.1 registry).

type refreshRecoveryDetails struct {
	CurrentRevision string `json:"current_revision"`
	Recovery        string `json:"recovery"`
}

type repreviewRecoveryDetails struct {
	Recovery           string            `json:"recovery"`
	CurrentRevision    *string           `json:"current_revision"`
	CurrentGenerations map[string]string `json:"current_generations"`
}

type cutoffSuggestionDetails struct {
	Recovery         string `json:"recovery"`
	CutoffSuggestion any    `json:"cutoff_suggestion"`
}

type operationIDConflictDetails struct {
	OperationID string `json:"operation_id"`
	Recovery    string `json:"recovery"`
}

type retryDetails struct {
	Recovery          string `json:"recovery"`
	RetryAfterSeconds *int   `json:"retry_after_seconds"`
}

type authTransitionDetails struct {
	TransitionState     string `json:"transition_state"`
	EffectiveGeneration string `json:"effective_generation"`
	Recovery            string `json:"recovery"`
	RetryAfterSeconds   *int   `json:"retry_after_seconds"`
}

// settingsSchemaFinalizingDetails is the exact ERR-08 wire (SPEC §4.1):
// params is exactly {}, details carries the four fixed fields and
// Retry-After: 3 matches details.retry_after_seconds.
type settingsSchemaFinalizingDetails struct {
	TransitionPhase   string `json:"transition_phase"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
	RetryScope        string `json:"retry_scope"`
	Recovery          string `json:"recovery"`
}

const settingsSchemaFinalizingRetryAfterSeconds = 3

// Problem registry: every code emitted by the settings surface is listed here
// with its status, registered details shape and retryability. Golden tests
// iterate this registry and reject unregistered codes or legacy shapes.
type problemRegistration struct {
	Code        string
	Status      int
	Retryable   bool
	DetailsKind string // registry description used by golden tests
}

var settingsProblemRegistry = map[string]problemRegistration{
	"validation_failed":                              {Code: "validation_failed", Status: http.StatusUnprocessableEntity, DetailsKind: "violations"},
	"invalid_retention_policy":                       {Code: "invalid_retention_policy", Status: http.StatusUnprocessableEntity, DetailsKind: "violations"},
	"invalid_retention_cutoff":                       {Code: "invalid_retention_cutoff", Status: http.StatusUnprocessableEntity, DetailsKind: "violations"},
	"invalid_audit_policy":                           {Code: "invalid_audit_policy", Status: http.StatusUnprocessableEntity, DetailsKind: "violations"},
	"invalid_operator_account":                       {Code: "invalid_operator_account", Status: http.StatusUnprocessableEntity, DetailsKind: "violations"},
	"invalid_timezone":                               {Code: "invalid_timezone", Status: http.StatusUnprocessableEntity, DetailsKind: "violations"},
	"unknown_field":                                  {Code: "unknown_field", Status: http.StatusUnprocessableEntity, DetailsKind: "violations"},
	"costing_settings_changed":                       {Code: "costing_settings_changed", Status: http.StatusConflict, DetailsKind: "refresh"},
	"retention_settings_changed":                     {Code: "retention_settings_changed", Status: http.StatusConflict, DetailsKind: "refresh"},
	"audit_settings_changed":                         {Code: "audit_settings_changed", Status: http.StatusConflict, DetailsKind: "refresh"},
	"auth_settings_changed":                          {Code: "auth_settings_changed", Status: http.StatusConflict, DetailsKind: "refresh"},
	"retention_owner_drift_changed":                  {Code: "retention_owner_drift_changed", Status: http.StatusConflict, DetailsKind: "repreview"},
	"retention_preflight_required":                   {Code: "retention_preflight_required", Status: http.StatusPreconditionRequired, DetailsKind: "repreview"},
	"retention_preflight_stale":                      {Code: "retention_preflight_stale", Status: http.StatusConflict, DetailsKind: "repreview"},
	"cutoff_unavailable_for_rollup":                  {Code: "cutoff_unavailable_for_rollup", Status: http.StatusUnprocessableEntity, DetailsKind: "cutoff_suggestion"},
	"operation_id_conflict":                          {Code: "operation_id_conflict", Status: http.StatusConflict, DetailsKind: "operation"},
	"retention_job_conflict":                         {Code: "retention_job_conflict", Status: http.StatusConflict, DetailsKind: "operation"},
	"job_terminal":                                   {Code: "job_terminal", Status: http.StatusConflict, DetailsKind: "operation"},
	"legacy_job_not_cancellable":                     {Code: "legacy_job_not_cancellable", Status: http.StatusConflict, DetailsKind: "operation"},
	"purge_not_cancellable":                          {Code: "purge_not_cancellable", Status: http.StatusConflict, DetailsKind: "operation"},
	"auth_readiness_changed":                         {Code: "auth_readiness_changed", Status: http.StatusConflict, DetailsKind: "auth_readiness"},
	"auth_acknowledgement_required":                  {Code: "auth_acknowledgement_required", Status: http.StatusConflict, DetailsKind: "auth_readiness"},
	"auth_readiness_unavailable":                     {Code: "auth_readiness_unavailable", Status: http.StatusServiceUnavailable, DetailsKind: "auth_readiness"},
	"auth_transition_in_progress":                    {Code: "auth_transition_in_progress", Status: http.StatusConflict, DetailsKind: "auth_transition"},
	"auth_transition_recovery_required":              {Code: "auth_transition_recovery_required", Status: http.StatusServiceUnavailable, DetailsKind: "auth_transition"},
	"auth_transition_fail_closed":                    {Code: "auth_transition_fail_closed", Status: http.StatusServiceUnavailable, DetailsKind: "auth_transition"},
	"CURRENCY_MIGRATION_REQUIRED":                    {Code: "CURRENCY_MIGRATION_REQUIRED", Status: http.StatusConflict, DetailsKind: "migration_entry"},
	"currency_migration_required":                    {Code: "currency_migration_required", Status: http.StatusConflict, DetailsKind: "migration_entry"},
	"currency_migration_stale":                       {Code: "currency_migration_stale", Status: http.StatusConflict, DetailsKind: "repreview"},
	"currency_migration_operation_conflict":          {Code: "currency_migration_operation_conflict", Status: http.StatusConflict, DetailsKind: "operation"},
	"currency_migration_draft_conflict":              {Code: "currency_migration_draft_conflict", Status: http.StatusConflict, DetailsKind: "operation"},
	"currency_migration_draft_expired":               {Code: "currency_migration_draft_expired", Status: http.StatusConflict, DetailsKind: "operation"},
	"currency_migration_draft_stale":                 {Code: "currency_migration_draft_stale", Status: http.StatusConflict, DetailsKind: "repreview"},
	"currency_migration_draft_corrupt":               {Code: "currency_migration_draft_corrupt", Status: http.StatusConflict, DetailsKind: "operation"},
	"currency_migration_draft_duplicate_template":    {Code: "currency_migration_draft_duplicate_template", Status: http.StatusConflict, DetailsKind: "operation"},
	"currency_migration_draft_template_set_changed":  {Code: "currency_migration_draft_template_set_changed", Status: http.StatusConflict, DetailsKind: "repreview"},
	"currency_migration_inventory_conflict":          {Code: "currency_migration_inventory_conflict", Status: http.StatusConflict, DetailsKind: "repreview"},
	"currency_migration_blocked_by_tiered_templates": {Code: "currency_migration_blocked_by_tiered_templates", Status: http.StatusConflict, DetailsKind: "tiered_templates"},
	"currency_migration_inventory_stale":             {Code: "currency_migration_inventory_stale", Status: http.StatusConflict, DetailsKind: "repreview"},
	"currency_migration_invalid_kind":                {Code: "currency_migration_invalid_kind", Status: http.StatusConflict, DetailsKind: "migration_entry"},
	"currency_migration_owner_unavailable":           {Code: "currency_migration_owner_unavailable", Status: http.StatusServiceUnavailable, Retryable: true, DetailsKind: "retry"},
	"settings_schema_finalizing":                     {Code: "settings_schema_finalizing", Status: http.StatusServiceUnavailable, DetailsKind: "schema_finalizing"},
	"settings_owner_unavailable":                     {Code: "settings_owner_unavailable", Status: http.StatusServiceUnavailable, Retryable: true, DetailsKind: "retry"},
	"internal_error":                                 {Code: "internal_error", Status: http.StatusInternalServerError, DetailsKind: "empty_object"},
}

// settingsSchemaFinalizingProblem returns the exact ERR-08 problem for the
// registered guarded mutation routes (SPEC §4.1): status 503, params exactly
// {}, details with transition_phase, retry_after_seconds=3,
// retry_scope="status_check_only" and recovery="wait_then_refetch_before_resubmit".
func settingsSchemaFinalizingProblem(transitionPhase string, requestIDValue string) SettingsProblem {
	return SettingsProblem{
		Code:   "settings_schema_finalizing",
		Detail: "Settings schema is quiescing/finalizing; refetch authoritative state before resubmitting",
		Params: map[string]any{},
		Details: settingsSchemaFinalizingDetails{
			TransitionPhase:   transitionPhase,
			RetryAfterSeconds: settingsSchemaFinalizingRetryAfterSeconds,
			RetryScope:        "status_check_only",
			Recovery:          "wait_then_refetch_before_resubmit",
		},
		RequestID: requestIDValue,
	}
}

func writeProblem(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, problem SettingsProblem, status int) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	if problem.RequestID == "" {
		problem.RequestID = middleware.GetReqID(r.Context())
	}
	if problem.Params == nil {
		problem.Params = map[string]any{}
	}
	if problem.Details == nil {
		switch {
		case problem.Code == "validation_failed", problem.Code == "unknown_field", strings.HasPrefix(problem.Code, "invalid_"):
			problem.Details = map[string]any{"violations": []any{}}
		default:
			problem.Details = struct{}{}
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}

// writeSettingsJSON gives every settings read/write response the same
// private no-store policy. Settings contain live revisions, readiness and
// recovery state; browser or intermediary caching would create stale safety
// decisions and is not an acceptable source of truth.
func writeSettingsJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "private, no-store")
	responseutil.WriteJSON(w, status, payload)
}

// writeSettingsDomainError converges the older settings implementation's
// validation/CAS errors onto the target flat envelope without exposing SQL or
// implementation details. New settings routes use this adapter until their
// domain validators are fully expressed as SettingsProblem values.
func writeSettingsDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		writeSettingsInternalError(w, r, corsSnapshot, err)
		return
	}
	detail := domainErr.Error()
	code := "validation_failed"
	status := domainErr.StatusCode
	var details any = map[string]any{"violations": []any{}}
	if blocked, ok := domainErr.Detail.(currencyMigrationTieredTemplatesDetail); ok {
		code = "currency_migration_blocked_by_tiered_templates"
		details = blocked
	}
	switch {
	case strings.Contains(detail, "currency_migration_operation_conflict"):
		code = "currency_migration_operation_conflict"
		details = map[string]any{"recovery": "new_operation"}
	case strings.Contains(detail, "currency_migration_draft_template_set_changed"):
		code = "currency_migration_draft_template_set_changed"
		details = map[string]any{"recovery": "create_new_draft"}
	case strings.Contains(detail, "currency_migration_draft_duplicate_template"):
		code = "currency_migration_draft_duplicate_template"
		details = map[string]any{"recovery": "upload_corrected_chunk"}
	case strings.Contains(detail, "currency_migration_draft_corrupt"):
		code = "currency_migration_draft_corrupt"
		details = map[string]any{"recovery": "create_new_draft"}
	case strings.Contains(detail, "currency_migration_draft_stale"):
		code = "currency_migration_draft_stale"
		details = map[string]any{"recovery": "refresh_and_repreview"}
	case strings.Contains(detail, "currency_migration_inventory_conflict"):
		code = "currency_migration_inventory_conflict"
		details = map[string]any{"recovery": "refresh_inventory"}
	case strings.Contains(detail, "currency_migration_blocked_by_tiered_templates"):
		if _, ok := domainErr.Detail.(currencyMigrationTieredTemplatesDetail); !ok {
			code = "currency_migration_blocked_by_tiered_templates"
			details = map[string]any{"recovery": "clear_tiers_before_currency_migration"}
		}
	case strings.Contains(detail, "currency_migration_inventory_stale"):
		code = "currency_migration_inventory_stale"
		details = map[string]any{"recovery": "refresh_inventory"}
	case strings.Contains(detail, "currency_migration_invalid_kind"):
		code = "currency_migration_invalid_kind"
		details = map[string]any{"recovery": "open_currency_migration"}
	case strings.Contains(detail, "currency_migration_draft_expired"):
		code = "currency_migration_draft_expired"
		details = map[string]any{"recovery": "create_new_draft"}
	case strings.Contains(detail, "currency_migration_draft_conflict"):
		code = "currency_migration_draft_conflict"
		details = map[string]any{"recovery": "refetch_draft"}
	case strings.Contains(detail, "currency_migration_draft_state_"):
		code = "currency_migration_draft_conflict"
		details = map[string]any{"recovery": "refetch_draft"}
	case strings.Contains(detail, "currency_migration_required"):
		code = "currency_migration_required"
		details = map[string]any{"recovery": "open_currency_migration"}
	case strings.Contains(detail, "currency_migration_owner_unavailable"):
		code = "currency_migration_owner_unavailable"
		details = retryDetails{Recovery: "retry", RetryAfterSeconds: intPtr(5)}
	case strings.Contains(detail, "currency_migration_stale"):
		code = "currency_migration_stale"
		details = map[string]any{"recovery": "refresh_and_repreview"}
	case strings.Contains(detail, "costing_settings_changed") || strings.Contains(detail, "currency_migration_stale"):
		code = "costing_settings_changed"
		currentUpdatedAt := ""
		if changed, ok := domainErr.Detail.(costingSettingsChangedDetail); ok {
			currentUpdatedAt = changed.CurrentUpdatedAt
		}
		details = map[string]any{"current_updated_at": currentUpdatedAt, "recovery": "refresh"}
	case strings.Contains(detail, "unknown_field"):
		code = "unknown_field"
	case strings.Contains(detail, "timezone"):
		code = "invalid_timezone"
	case status >= http.StatusInternalServerError:
		code = "internal_error"
		details = struct{}{}
	}
	writeProblem(w, r, corsSnapshot, SettingsProblem{Code: code, Detail: detail, Params: map[string]any{}, Details: details}, status)
}

// problemf builds a registered problem with the given params and details.
func problemf(code string, detail string, params map[string]any, details any) (SettingsProblem, int) {
	registration, ok := settingsProblemRegistry[code]
	if !ok {
		// Registry exhaustiveness is a golden test; this fallback must never
		// fire for settings-owned codes.
		return SettingsProblem{Code: code, Detail: detail, Params: params, Details: details}, http.StatusInternalServerError
	}
	return SettingsProblem{Code: code, Detail: detail, Params: params, Details: details}, registration.Status
}

func writeSettingsInternalError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	if err != nil {
		slog.Error("settings internal error", "error", err)
	}
	writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "internal_error", Detail: "Internal server error", Params: map[string]any{}}, http.StatusInternalServerError)
}
