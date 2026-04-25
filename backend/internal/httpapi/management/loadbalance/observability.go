package loadbalance

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s *Service) handleListCurrentState(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := parseRequiredPositiveIntQuery(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalancedomain.CurrentStateListResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalancedomain.CurrentStateListResponse{}, err
		}
		return loadbalancedomain.ListCurrentState(r.Context(), tx, s.runtimeState, profile.ID, modelConfigID, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleResetCurrentState(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalancedomain.CurrentStateResetResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalancedomain.CurrentStateResetResponse{}, err
		}
		return loadbalancedomain.ResetCurrentState(r.Context(), tx, s.runtimeState, profile.ID, connectionID)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleListEvents(w http.ResponseWriter, r *http.Request) {
	modelID := strings.TrimSpace(r.URL.Query().Get("model_id"))
	if modelID == "" {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "model_id is required")
		return
	}
	limit, err := parsePositiveIntQueryWithDefault(r, "limit", 50)
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	offset, err := parseNonNegativeIntQueryWithDefault(r, "offset", 0)
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalancedomain.EventListResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalancedomain.EventListResponse{}, err
		}
		return loadbalancedomain.ListEvents(r.Context(), tx, profile.ID, modelID, limit, offset)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	eventID, err := routeInt64(r, "event_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (*loadbalancedomain.EventDetail, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		return loadbalancedomain.GetEvent(r.Context(), tx, profile.ID, eventID)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	if response == nil {
		writeError(w, r, s.allowedOrigins, http.StatusNotFound, "Loadbalance event not found")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteEvents(w http.ResponseWriter, r *http.Request) {
	_, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (struct{}, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return struct{}{}, err
		}
		before, err := parseOptionalTimeQuery(r, "before")
		if err != nil {
			return struct{}{}, err
		}
		olderThanDays, err := parseOptionalPositiveIntQuery(r, "older_than_days")
		if err != nil {
			return struct{}{}, err
		}
		deleteAll, err := parseOptionalBoolQuery(r, "delete_all")
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, loadbalancedomain.DeleteEvents(r.Context(), tx, loadbalancedomain.DeleteParams{ProfileID: profile.ID, Before: before, OlderThanDays: olderThanDays, DeleteAll: deleteAll, ReferenceNow: s.nowUTC()})
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func parseRequiredPositiveIntQuery(r *http.Request, key string) (int, error) {
	parsed, err := parseOptionalPositiveIntQuery(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return 0, fmt.Errorf("%s is required", key)
	}
	return *parsed, nil
}

func parseOptionalPositiveIntQuery(r *http.Request, key string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return nil, fmt.Errorf("invalid %s", key)
	}
	resolved := parsed
	return &resolved, nil
}

func parsePositiveIntQueryWithDefault(r *http.Request, key string, defaultValue int) (int, error) {
	parsed, err := parseOptionalPositiveIntQuery(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return defaultValue, nil
	}
	return *parsed, nil
}

func parseNonNegativeIntQueryWithDefault(r *http.Request, key string, defaultValue int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return parsed, nil
}

func parseOptionalBoolQuery(r *http.Request, key string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s", key)
	}
	return parsed, nil
}

func parseOptionalTimeQuery(r *http.Request, key string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, fmt.Errorf("invalid %s", key)
		}
	}
	resolved := parsed.UTC()
	return &resolved, nil
}

func routeInt64(request *http.Request, name string) (int64, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}
