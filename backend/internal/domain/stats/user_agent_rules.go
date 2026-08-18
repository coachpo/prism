package stats

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
)

type compiledUserAgentRule struct {
	ID         int
	Name       string
	RawPattern string
	Pattern    *regexp.Regexp
}

func loadCompiledUserAgentRules(ctx context.Context, exec queryExecutor, profileID int) ([]compiledUserAgentRule, error) {
	rows, err := exec.Query(ctx, `SELECT id, name, pattern FROM user_agent_client_rules WHERE enabled = TRUE AND (profile_id = $1 OR is_system = TRUE) ORDER BY is_system ASC, id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query user-agent client rules for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]compiledUserAgentRule, 0)
	for rows.Next() {
		var id int
		var name string
		var pattern string
		if err := rows.Scan(&id, &name, &pattern); err != nil {
			return nil, fmt.Errorf("scan user-agent client rule: %w", err)
		}
		compiled, compileErr := regexp.Compile("(?i)" + pattern)
		if compileErr != nil {
			continue
		}
		items = append(items, compiledUserAgentRule{ID: id, Name: name, RawPattern: pattern, Pattern: compiled})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user-agent client rules for profile %d: %w", profileID, err)
	}
	return items, nil
}

func loadCompiledUserAgentRuleByID(ctx context.Context, exec queryExecutor, profileID int, ruleID int) (compiledUserAgentRule, bool, error) {
	var id int
	var name string
	var pattern string
	err := exec.QueryRow(ctx, `SELECT id, name, pattern FROM user_agent_client_rules WHERE id = $1 AND enabled = TRUE AND (profile_id = $2 OR is_system = TRUE)`, ruleID, profileID).Scan(&id, &name, &pattern)
	if err == pgx.ErrNoRows {
		return compiledUserAgentRule{}, false, nil
	}
	if err != nil {
		return compiledUserAgentRule{}, false, fmt.Errorf("load user-agent client rule %d for profile %d: %w", ruleID, profileID, err)
	}
	compiled, compileErr := regexp.Compile("(?i)" + pattern)
	if compileErr != nil {
		return compiledUserAgentRule{}, false, fmt.Errorf("compile user-agent client rule %d: %w", ruleID, compileErr)
	}
	return compiledUserAgentRule{ID: id, Name: name, RawPattern: pattern, Pattern: compiled}, true, nil
}

func classifyUserAgentDisplay(userAgent *string, rules []compiledUserAgentRule) *string {
	if userAgent == nil {
		return nil
	}
	for _, rule := range rules {
		if rule.Pattern.MatchString(*userAgent) {
			resolved := rule.Name
			return &resolved
		}
	}
	resolved := *userAgent
	return &resolved
}

func userAgentOverridden(callerUserAgent *string, upstreamUserAgent *string) bool {
	if callerUserAgent == nil && upstreamUserAgent == nil {
		return false
	}
	if callerUserAgent == nil || upstreamUserAgent == nil {
		return true
	}
	return *callerUserAgent != *upstreamUserAgent
}
