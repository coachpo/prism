package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/go-chi/chi/v5/middleware"
)

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	setNoStoreHeaders(w)
	if authErr, ok := errors.AsType[*domainError](err); ok {
		if authErr.Code != "" && (strings.HasPrefix(authErr.Code, "auth_") || authErr.Code == "invalid_operator_account" || authErr.Code == "operation_id_conflict") {
			platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			details := authErr.Fields["details"]
			if details == nil {
				details = map[string]any{}
			}
			w.WriteHeader(authErr.StatusCode)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":       authErr.Code,
				"detail":     authErr.Detail,
				"params":     map[string]any{},
				"details":    details,
				"request_id": middleware.GetReqID(r.Context()),
			})
			return
		}
		responseutil.WriteErrorFields(w, r, corsSnapshot, authErr.StatusCode, authErr.Detail, domainErrorFields(authErr))
		return
	}
	slog.Error("auth handler internal error", "error", err)
	responseutil.WriteError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func domainErrorFields(err *domainError) map[string]any {
	fields := make(map[string]any, len(err.Fields)+1)
	if err.Code != "" {
		fields["code"] = err.Code
	}
	for key, value := range err.Fields {
		fields[key] = value
	}
	return fields
}

// writeAuthProblem writes a registered auth problem through the shared flat
// management envelope {code, detail, params, details, request_id} and sets
// the same-source Retry-After header for auth_login_locked. The wire params
// are always the registered exact empty object. Errors without a registered
// auth code keep the legacy writer until their owner converges them.
func writeAuthProblem(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err *domainError) {
	if err == nil {
		return
	}
	if _, ok := lookupAuthProblemEntry(err.Code); !ok {
		writeDomainError(w, r, corsSnapshot, err)
		return
	}
	params := authProblemParams(err.Code)
	var details any
	if err.Details != nil {
		details = err.Details
	}
	if err.Code == ProblemCodeAuthLoginLocked {
		if locked, ok := details.(AuthLoginLockedDetails); ok {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", locked.RetryAfterSeconds))
		}
	}
	responseutil.WriteProblem(w, r, corsSnapshot, err.StatusCode, err.Code, err.Detail, params, details)
}

func transitionProblemCodeFor(state string) string {
	if state == "rollback_required" {
		return ProblemCodeAuthTransitionRecoveryNeeded
	}
	return ProblemCodeAuthTransitionInProgress
}

func transitionProblemDetailFor(state string) string {
	if state == "rollback_required" {
		return "正在恢复上一份有效配置"
	}
	return "正在安全启用，管理访问暂不可用"
}

// writeTransitionProblem writes the registered typed 503 for a persisted
// fail-closed auth transition, with the same-source optional Retry-After.
func writeTransitionProblem(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, state string, generation int64, retryAfter *int64) {
	code := transitionProblemCodeFor(state)
	details := transitionProblemDetailsFor(state, generation, retryAfter)
	if retryAfter != nil {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", *retryAfter))
	}
	responseutil.WriteProblem(w, r, corsSnapshot, http.StatusServiceUnavailable, code, transitionProblemDetailFor(state), authProblemParams(code), details)
}

func authSettingsProblem(status int, code, detail string, fields map[string]any) error {
	return &domainError{StatusCode: status, Code: code, Detail: detail, Fields: fields}
}

func writeAuthSettingsV2Problem(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, status int, code, detail string, details any) {
	setNoStoreHeaders(w)
	responseutil.WriteProblem(w, r, corsSnapshot, status, code, detail, map[string]any{}, details)
}
