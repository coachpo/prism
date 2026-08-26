package configrules

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

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
