package registry

import (
	"fmt"
	"strings"
)

type ValidationIssue struct {
	Code   string
	Field  string
	Detail string
}

type ValidationError struct {
	Record string
	Issues []ValidationIssue
}

func (err ValidationError) Error() string {
	parts := make([]string, 0, len(err.Issues))
	for _, issue := range err.Issues {
		parts = append(parts, fmt.Sprintf("%s %s: %s", issue.Code, issue.Field, issue.Detail))
	}
	return fmt.Sprintf("registry validation failed for %s: %s", err.Record, strings.Join(parts, "; "))
}

func (err ValidationError) HasCode(code string) bool {
	for _, issue := range err.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func newValidationError(record string, issues []ValidationIssue) error {
	if len(issues) == 0 {
		return nil
	}
	cloned := make([]ValidationIssue, len(issues))
	copy(cloned, issues)
	return ValidationError{Record: strings.TrimSpace(record), Issues: cloned}
}

func issue(code string, field string, detail string) ValidationIssue {
	return ValidationIssue{Code: strings.TrimSpace(code), Field: strings.TrimSpace(field), Detail: strings.TrimSpace(detail)}
}

func appendBlankIssue(issues []ValidationIssue, code string, field string, detail string, value string) []ValidationIssue {
	if strings.TrimSpace(value) == "" {
		return append(issues, issue(code, field, detail))
	}
	return issues
}
