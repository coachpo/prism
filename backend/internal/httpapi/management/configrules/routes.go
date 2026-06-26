package configrules

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

var headerTokenRE = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*$`)

func (s *Service) handleListHeaderBlocklistRules(w http.ResponseWriter, r *http.Request) {
	includeDisabled, err := parseBooleanQuery(r, "include_disabled", true)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config rules", func(tx pgx.Tx) ([]headerBlocklistRuleResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		rows, err := listHeaderBlocklistRules(r.Context(), tx, profile.ID, includeDisabled)
		if err != nil {
			return nil, err
		}
		response := make([]headerBlocklistRuleResponse, 0, len(rows))
		for _, row := range rows {
			response = append(response, headerBlocklistRuleResponse(row))
		}
		return response, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetHeaderBlocklistRule(w http.ResponseWriter, r *http.Request) {
	ruleID, err := routeInt(r, "rule_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config rules", func(tx pgx.Tx) (headerBlocklistRuleResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return headerBlocklistRuleResponse{}, err
		}
		row, found, err := loadHeaderBlocklistRule(r.Context(), tx, profile.ID, ruleID, false)
		if err != nil {
			return headerBlocklistRuleResponse{}, err
		}
		if !found {
			return headerBlocklistRuleResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Header blocklist rule not found"}
		}
		return headerBlocklistRuleResponse(row), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateHeaderBlocklistRule(w http.ResponseWriter, r *http.Request) {
	var requestBody headerBlocklistRuleCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateHeaderBlocklistCreate(&requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config rules", func(tx pgx.Tx) (headerBlocklistRuleResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return headerBlocklistRuleResponse{}, err
		}
		duplicate, err := findHeaderBlocklistDuplicate(r.Context(), tx, profile.ID, requestBody.MatchType, requestBody.Pattern, nil)
		if err != nil {
			return headerBlocklistRuleResponse{}, err
		}
		if duplicate {
			return headerBlocklistRuleResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Rule with match_type='%s' and pattern='%s' already exists", requestBody.MatchType, requestBody.Pattern)}
		}
		created, err := insertHeaderBlocklistRule(r.Context(), tx, profile.ID, requestBody, s.nowUTC())
		if err != nil {
			return headerBlocklistRuleResponse{}, err
		}
		return headerBlocklistRuleResponse(created), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateHeaderBlocklistRule(w http.ResponseWriter, r *http.Request) {
	ruleID, err := routeInt(r, "rule_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody headerBlocklistRuleUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateHeaderBlocklistUpdate(&requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config rules", func(tx pgx.Tx) (headerBlocklistRuleResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return headerBlocklistRuleResponse{}, err
		}
		row, found, err := loadHeaderBlocklistRule(r.Context(), tx, profile.ID, ruleID, true)
		if err != nil {
			return headerBlocklistRuleResponse{}, err
		}
		if !found {
			return headerBlocklistRuleResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Header blocklist rule not found"}
		}
		if row.IsSystem {
			attempted := make([]string, 0, 3)
			if requestBody.Name.Set {
				attempted = append(attempted, "name")
			}
			if requestBody.MatchType.Set {
				attempted = append(attempted, "match_type")
			}
			if requestBody.Pattern.Set {
				attempted = append(attempted, "pattern")
			}
			if len(attempted) > 0 {
				return headerBlocklistRuleResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Cannot modify %s on a system rule. Only 'enabled' is mutable.", strings.Join(attempted, ", "))}
			}
		}
		next := row
		if requestBody.Name.Set {
			next.Name = stringValue(requestBody.Name.Value)
		}
		if requestBody.MatchType.Set {
			next.MatchType = stringValue(requestBody.MatchType.Value)
		}
		if requestBody.Pattern.Set {
			next.Pattern = stringValue(requestBody.Pattern.Value)
		}
		if err := normalizeAndValidateHeaderRuleShape(&next.MatchType, &next.Pattern); err != nil {
			return headerBlocklistRuleResponse{}, err
		}
		duplicate, err := findHeaderBlocklistDuplicate(r.Context(), tx, profile.ID, next.MatchType, next.Pattern, &row.ID)
		if err != nil {
			return headerBlocklistRuleResponse{}, err
		}
		if duplicate {
			return headerBlocklistRuleResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Rule with match_type='%s' and pattern='%s' already exists", next.MatchType, next.Pattern)}
		}
		if requestBody.Enabled.Set {
			next.Enabled = requestBody.Enabled.Value
		}
		next.UpdatedAt = s.nowUTC()
		updated, err := updateHeaderBlocklistRule(r.Context(), tx, next)
		if err != nil {
			return headerBlocklistRuleResponse{}, err
		}
		return headerBlocklistRuleResponse(updated), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteHeaderBlocklistRule(w http.ResponseWriter, r *http.Request) {
	ruleID, err := routeInt(r, "rule_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config rules", func(tx pgx.Tx) (deletedResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return deletedResponse{}, err
		}
		row, found, err := loadOwnedHeaderBlocklistRule(r.Context(), tx, profile.ID, ruleID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Header blocklist rule not found"}
		}
		if row.IsSystem {
			return deletedResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Cannot delete a system rule. Disable it instead."}
		}
		if err := deleteHeaderBlocklistRule(r.Context(), tx, row.ID); err != nil {
			return deletedResponse{}, err
		}
		return deletedResponse{Deleted: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleListUserAgentClientRules(w http.ResponseWriter, r *http.Request) {
	includeDisabled, err := parseBooleanQuery(r, "include_disabled", true)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config rules", func(tx pgx.Tx) ([]userAgentClientRuleResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		rows, err := listUserAgentClientRules(r.Context(), tx, profile.ID, includeDisabled)
		if err != nil {
			return nil, err
		}
		response := make([]userAgentClientRuleResponse, 0, len(rows))
		for _, row := range rows {
			response = append(response, userAgentClientRuleResponse(row))
		}
		return response, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetUserAgentClientRule(w http.ResponseWriter, r *http.Request) {
	ruleID, err := routeInt(r, "rule_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config rules", func(tx pgx.Tx) (userAgentClientRuleResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return userAgentClientRuleResponse{}, err
		}
		row, found, err := loadUserAgentClientRule(r.Context(), tx, profile.ID, ruleID, false)
		if err != nil {
			return userAgentClientRuleResponse{}, err
		}
		if !found {
			return userAgentClientRuleResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "User agent client rule not found"}
		}
		return userAgentClientRuleResponse(row), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateUserAgentClientRule(w http.ResponseWriter, r *http.Request) {
	var requestBody userAgentClientRuleCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateUserAgentCreate(&requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config rules", func(tx pgx.Tx) (userAgentClientRuleResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return userAgentClientRuleResponse{}, err
		}
		created, err := insertUserAgentClientRule(r.Context(), tx, profile.ID, requestBody, s.nowUTC())
		if err != nil {
			return userAgentClientRuleResponse{}, err
		}
		return userAgentClientRuleResponse(created), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateUserAgentClientRule(w http.ResponseWriter, r *http.Request) {
	ruleID, err := routeInt(r, "rule_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody userAgentClientRuleUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateUserAgentUpdate(&requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config rules", func(tx pgx.Tx) (userAgentClientRuleResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return userAgentClientRuleResponse{}, err
		}
		row, found, err := loadUserAgentClientRule(r.Context(), tx, profile.ID, ruleID, true)
		if err != nil {
			return userAgentClientRuleResponse{}, err
		}
		if !found {
			return userAgentClientRuleResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "User agent client rule not found"}
		}
		if row.IsSystem {
			attempted := make([]string, 0, 2)
			if requestBody.Name.Set {
				attempted = append(attempted, "name")
			}
			if requestBody.Pattern.Set {
				attempted = append(attempted, "pattern")
			}
			if len(attempted) > 0 {
				return userAgentClientRuleResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Cannot modify %s on a system rule. Only 'enabled' is mutable.", strings.Join(attempted, ", "))}
			}
		}
		next := row
		if requestBody.Name.Set {
			next.Name = stringValue(requestBody.Name.Value)
		}
		if requestBody.Pattern.Set {
			next.Pattern = stringValue(requestBody.Pattern.Value)
		}
		if requestBody.Enabled.Set {
			next.Enabled = requestBody.Enabled.Value
		}
		next.UpdatedAt = s.nowUTC()
		updated, err := updateUserAgentClientRule(r.Context(), tx, next)
		if err != nil {
			return userAgentClientRuleResponse{}, err
		}
		return userAgentClientRuleResponse(updated), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteUserAgentClientRule(w http.ResponseWriter, r *http.Request) {
	ruleID, err := routeInt(r, "rule_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config rules", func(tx pgx.Tx) (deletedResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return deletedResponse{}, err
		}
		row, found, err := loadOwnedUserAgentClientRule(r.Context(), tx, profile.ID, ruleID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "User agent client rule not found"}
		}
		if row.IsSystem {
			return deletedResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Cannot delete a system rule. Disable it instead."}
		}
		if err := deleteUserAgentClientRule(r.Context(), tx, row.ID); err != nil {
			return deletedResponse{}, err
		}
		return deletedResponse{Deleted: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func normalizeAndValidateHeaderBlocklistCreate(requestBody *headerBlocklistRuleCreateRequest) error {
	return normalizeAndValidateHeaderRuleShape(&requestBody.MatchType, &requestBody.Pattern)
}

func normalizeAndValidateHeaderBlocklistUpdate(requestBody *headerBlocklistRuleUpdateRequest) error {
	if requestBody.MatchType.Set && requestBody.MatchType.Value != nil {
		normalized := strings.ToLower(strings.TrimSpace(*requestBody.MatchType.Value))
		requestBody.MatchType.Value = &normalized
		if normalized != "exact" && normalized != "prefix" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "match_type must be 'exact' or 'prefix'"}
		}
	}
	if requestBody.Pattern.Set && requestBody.Pattern.Value != nil {
		normalized := strings.ToLower(strings.TrimSpace(*requestBody.Pattern.Value))
		requestBody.Pattern.Value = &normalized
		if normalized == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must not be empty"}
		}
		if !headerTokenRE.MatchString(normalized) {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must contain only lowercase alphanumeric characters and hyphens, and must start with an alphanumeric character"}
		}
	}
	return nil
}

func normalizeAndValidateHeaderRuleShape(matchType *string, pattern *string) error {
	*matchType = strings.ToLower(strings.TrimSpace(*matchType))
	if *matchType != "exact" && *matchType != "prefix" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "match_type must be 'exact' or 'prefix'"}
	}
	*pattern = strings.ToLower(strings.TrimSpace(*pattern))
	if *pattern == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must not be empty"}
	}
	if !headerTokenRE.MatchString(*pattern) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must contain only lowercase alphanumeric characters and hyphens, and must start with an alphanumeric character"}
	}
	if *matchType == "prefix" && !strings.HasSuffix(*pattern, "-") {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "prefix pattern must end with '-'"}
	}
	return nil
}

func normalizeAndValidateUserAgentCreate(requestBody *userAgentClientRuleCreateRequest) error {
	requestBody.Name = strings.TrimSpace(requestBody.Name)
	if requestBody.Name == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "name must not be empty"}
	}
	requestBody.Pattern = strings.TrimSpace(requestBody.Pattern)
	if requestBody.Pattern == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must not be empty"}
	}
	if _, err := regexp.Compile(requestBody.Pattern); err != nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must be a valid regular expression"}
	}
	return nil
}

func normalizeAndValidateUserAgentUpdate(requestBody *userAgentClientRuleUpdateRequest) error {
	if requestBody.Name.Set && requestBody.Name.Value != nil {
		normalized := strings.TrimSpace(*requestBody.Name.Value)
		requestBody.Name.Value = &normalized
		if normalized == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "name must not be empty"}
		}
	}
	if requestBody.Pattern.Set && requestBody.Pattern.Value != nil {
		normalized := strings.TrimSpace(*requestBody.Pattern.Value)
		requestBody.Pattern.Value = &normalized
		if normalized == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must not be empty"}
		}
		if _, err := regexp.Compile(normalized); err != nil {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must be a valid regular expression"}
		}
	}
	return nil
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	return json.NewDecoder(request.Body).Decode(target)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var configErr *domainError
	if errors.As(err, &configErr) {
		responseutil.WriteError(w, r, corsSnapshot, configErr.StatusCode, configErr.Detail)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	responseutil.WriteError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func routeInt(request *http.Request, name string) (int, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

func parseBooleanQuery(request *http.Request, name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(request.URL.Query().Get(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("%s must be a boolean", name)}
	}
	return parsed, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
