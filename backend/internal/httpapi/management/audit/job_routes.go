package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s *Service) handleGetJob(w http.ResponseWriter, r *http.Request) {
	writeJobNoStore(w)
	if hasGlobalRetentionJobQuery(r) && !isGlobalLogRetentionRequest(r) {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "scope=global and type=log_retention are required together")
		return
	}
	if isGlobalLogRetentionRequest(r) {
		if err := validateGlobalRetentionJobQuery(r, "detail"); err != nil {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
			return
		}
		detail, err := s.jobs.GetGlobalRetentionJobDetailDTO(r.Context(), chi.URLParam(r, "job_id"))
		if err != nil {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Job not found")
			return
		}
		responseutil.WriteJSON(w, http.StatusOK, detail)
		return
	}
	s.withJob(w, r, func(job managementjobs.Job) { responseutil.WriteJSON(w, http.StatusOK, job) })
}

func (s *Service) handleGetJobCheckpoints(w http.ResponseWriter, r *http.Request) {
	writeJobNoStore(w)
	if !isGlobalLogRetentionRequest(r) {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Job not found")
		return
	}
	if err := validateGlobalRetentionJobQuery(r, "evidence"); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	limit, cursor, err := parseJobEvidencePageParams(r)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.jobs.GetGlobalRetentionJobCheckpointsDTO(r.Context(), chi.URLParam(r, "job_id"), limit, cursor)
	if err != nil {
		if managementjobs.IsInvalidJobsCursor(err) {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid cursor")
			return
		}
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Job not found")
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, page)
}

func (s *Service) handleGetJobPartitions(w http.ResponseWriter, r *http.Request) {
	writeJobNoStore(w)
	if !isGlobalLogRetentionRequest(r) {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Job not found")
		return
	}
	if err := validateGlobalRetentionJobQuery(r, "evidence"); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	limit, cursor, err := parseJobEvidencePageParams(r)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.jobs.GetGlobalRetentionJobPartitionsDTO(r.Context(), chi.URLParam(r, "job_id"), limit, cursor)
	if err != nil {
		if managementjobs.IsInvalidJobsCursor(err) {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid cursor")
			return
		}
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Job not found")
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, page)
}

func isGlobalLogRetentionRequest(r *http.Request) bool {
	return strings.TrimSpace(r.URL.Query().Get("scope")) == "global" &&
		strings.TrimSpace(r.URL.Query().Get("type")) == "log_retention"
}

func hasGlobalRetentionJobQuery(r *http.Request) bool {
	_, hasScope := r.URL.Query()["scope"]
	_, hasType := r.URL.Query()["type"]
	return hasScope || hasType
}

func validateGlobalRetentionJobQuery(r *http.Request, mode string) error {
	allowed := map[string]struct{}{
		"scope": {}, "type": {},
	}
	if mode == "evidence" {
		allowed["limit"] = struct{}{}
		allowed["cursor"] = struct{}{}
	} else if mode == "list" {
		allowed["origin"] = struct{}{}
		allowed["state"] = struct{}{}
		allowed["limit"] = struct{}{}
		allowed["cursor"] = struct{}{}
	}
	for key := range r.URL.Query() {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unsupported global retention job query parameter %q", key)
		}
	}
	if len(r.URL.Query()["scope"]) != 1 || len(r.URL.Query()["type"]) != 1 || !isGlobalLogRetentionRequest(r) {
		return fmt.Errorf("scope=global and type=log_retention are required exactly once")
	}
	return nil
}

func parseJobEvidencePageParams(r *http.Request) (int, string, error) {
	for key := range r.URL.Query() {
		switch key {
		case "scope", "type", "limit", "cursor":
		default:
			return 0, "", fmt.Errorf("unsupported job evidence query parameter %q", key)
		}
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return 0, "", fmt.Errorf("invalid limit")
		}
		limit = parsed
	}
	return limit, strings.TrimSpace(r.URL.Query().Get("cursor")), nil
}

func (s *Service) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	writeJobNoStore(w)
	jobID := chi.URLParam(r, "job_id")
	if hasGlobalRetentionJobQuery(r) && !isGlobalLogRetentionRequest(r) {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "scope=global and type=log_retention are required together")
		return
	}
	if isGlobalLogRetentionRequest(r) {
		if err := validateGlobalRetentionJobQuery(r, "detail"); err != nil {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
			return
		}
		var request struct {
			OperationID string `json:"operation_id"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, responseutil.SanitizeDecodeError(err).Error())
			return
		}
		if strings.TrimSpace(request.OperationID) == "" {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "operation_id is required")
			return
		}
		job, replayed, err := s.jobs.CancelGlobalRetentionJobDTO(r.Context(), jobID, request.OperationID)
		if err != nil {
			var ownerUnavailable *auditdomain.AffectedWriterUnavailableError
			switch {
			case errors.As(err, &ownerUnavailable):
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "retention_owner_unavailable")
			case managementjobs.IsRetentionCancelOperationConflict(err):
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusConflict, "operation_id_conflict")
			case managementjobs.IsLegacyJobNotCancellable(err):
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusConflict, "legacy_job_not_cancellable")
			case managementjobs.IsJobTerminal(err):
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusConflict, "job_terminal")
			case managementjobs.IsPurgeNotCancellable(err):
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusConflict, "purge_not_cancellable")
			case managementjobs.IsNotFound(err):
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Job not found")
			default:
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Internal server error")
			}
			return
		}
		responseutil.WriteJSON(w, http.StatusOK, map[string]any{
			"operation_id": request.OperationID,
			"replayed":     replayed,
			"job":          job,
		})
		return
	}
	profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), s.pool, r.Header.Get(profiledomain.ProfileIDHeader))
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	job, err := s.jobs.CancelJob(r.Context(), jobID, profile.ID)
	if err != nil && managementjobs.IsNotFound(err) {
		job, err = s.jobs.CancelGlobalLogRetentionJob(r.Context(), jobID)
	}
	if err != nil {
		if managementjobs.IsNotFound(err) {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Job not found")
			return
		}
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Internal server error")
		return
	}
	responseutil.WriteJSON(w, http.StatusAccepted, job)
}
func (s *Service) handleListJobs(w http.ResponseWriter, r *http.Request) {
	writeJobNoStore(w)
	if hasGlobalRetentionJobQuery(r) && !isGlobalLogRetentionRequest(r) {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "scope=global and type=log_retention are required together")
		return
	}
	if isGlobalLogRetentionRequest(r) {
		if err := validateGlobalRetentionJobQuery(r, "list"); err != nil {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
			return
		}
		origin := strings.TrimSpace(r.URL.Query().Get("origin"))
		if origin != "" && origin != "automatic" && origin != "manual" {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid origin")
			return
		}
		states := []string{}
		if raw := strings.TrimSpace(r.URL.Query().Get("state")); raw != "" {
			for _, item := range strings.Split(raw, ",") {
				state := strings.TrimSpace(item)
				switch state {
				case "queued", "running", "cancel_requested", "cancelled", "succeeded", "failed", "superseded":
					states = append(states, state)
				default:
					responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid state")
					return
				}
			}
		}
		limit := 20
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 100 {
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid limit")
				return
			}
			limit = parsed
		}
		var cursor *string
		if raw := strings.TrimSpace(r.URL.Query().Get("cursor")); raw != "" {
			cursor = &raw
		}
		response, err := s.jobs.ListGlobalRetentionJobsDTO(r.Context(), origin, states, limit, cursor)
		if err != nil {
			if managementjobs.IsInvalidJobsCursor(err) {
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid cursor")
				return
			}
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Internal server error")
			return
		}
		responseutil.WriteJSON(w, http.StatusOK, response)
		return
	}
	profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), s.pool, r.Header.Get(profiledomain.ProfileIDHeader))
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := s.jobs.ListJobs(r.Context(), profile.ID, 50)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Internal server error")
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func writeJobNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
}
func (s *Service) withJob(w http.ResponseWriter, r *http.Request, fn func(managementjobs.Job)) {
	profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), s.pool, r.Header.Get(profiledomain.ProfileIDHeader))
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	job, err := s.jobs.GetJob(r.Context(), chi.URLParam(r, "job_id"), profile.ID)
	if err != nil {
		job, err = s.jobs.GetGlobalJob(r.Context(), chi.URLParam(r, "job_id"))
	}
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Job not found")
		return
	}
	fn(job)
}
