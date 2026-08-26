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
