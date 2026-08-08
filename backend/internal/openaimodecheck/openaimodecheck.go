// Package openaimodecheck owns the read-only OpenAI text mode equality check
// shared by the upgrade preflight entrypoint and the startup fail-fast step.
//
// The check scans every persisted OpenAI relation in the default profile's
// planning scope — model to model and model to Terminal Target (connections,
// including standalone references) — and reports relations whose source mode
// differs from the target mode. Disabled or inactive relations are not exempt.
// It never writes management state and never contacts an upstream provider.
package openaimodecheck

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// RelationKind identifies the persisted relation shape behind a violation.
type RelationKind string

const (
	// RelationKindModelTarget is a model_access_targets row pointing at a
	// model_configs row.
	RelationKindModelTarget RelationKind = "model_target"
	// RelationKindConnectionTarget is a model_access_targets row pointing at a
	// connections row (owner-scoped private connection or standalone reference).
	RelationKindConnectionTarget RelationKind = "connection_target"
)

// Violation describes one persisted OpenAI relation with unequal modes.
type Violation struct {
	SourceModelID string
	RelationKind  RelationKind
	TargetID      string
	SourceMode    string
	TargetMode    string
}

// Report is the deterministic preflight result for one profile.
type Report struct {
	ProfileID  int
	Violations []Violation
}

// Count returns the number of violations.
func (report Report) Count() int {
	return len(report.Violations)
}

// Summary returns a one-line deterministic summary.
func (report Report) Summary() string {
	return fmt.Sprintf("openai_mode_preflight profile=%d violations=%d", report.ProfileID, len(report.Violations))
}

// String renders the deterministic full report: a summary line followed by one
// stable line per violation, sorted by source model, kind, and target id.
func (report Report) String() string {
	lines := make([]string, 0, len(report.Violations)+1)
	lines = append(lines, report.Summary())
	for _, violation := range report.Violations {
		lines = append(lines, fmt.Sprintf(
			"  - %s source=%s target=%s source_mode=%s target_mode=%s",
			violation.RelationKind, violation.SourceModelID, violation.TargetID,
			violation.SourceMode, violation.TargetMode,
		))
	}
	return strings.Join(lines, "\n")
}

type queryer interface {
	Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error)
}

// Check scans profileID for mode-equality violations. It is read-only and
// deterministic: violation order is stable regardless of execution timing.
func Check(ctx context.Context, queryer queryer, profileID int) (Report, error) {
	report := Report{ProfileID: profileID}

	rows, err := queryer.Query(ctx, `
		SELECT src.model_id, tgt.model_id,
			COALESCE(src.openai_accepted_format, ''),
			COALESCE(tgt.openai_accepted_format, '')
		FROM model_access_targets mat
		JOIN model_configs src ON src.id = mat.source_model_config_id
		JOIN model_configs tgt ON tgt.id = mat.target_model_config_id
		WHERE mat.profile_id = $1
			AND src.api_family = 'openai'
			AND src.openai_accepted_format IS DISTINCT FROM tgt.openai_accepted_format
		ORDER BY src.model_id ASC, tgt.model_id ASC, mat.id ASC`, profileID)
	if err != nil {
		return Report{}, fmt.Errorf("scan openai model-target mode violations: %w", err)
	}
	for rows.Next() {
		var violation Violation
		if err := rows.Scan(&violation.SourceModelID, &violation.TargetID, &violation.SourceMode, &violation.TargetMode); err != nil {
			rows.Close()
			return Report{}, fmt.Errorf("scan openai model-target violation row: %w", err)
		}
		violation.RelationKind = RelationKindModelTarget
		report.Violations = append(report.Violations, violation)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Report{}, fmt.Errorf("iterate openai model-target violations: %w", err)
	}
	rows.Close()

	rows, err = queryer.Query(ctx, `
		SELECT src.model_id,
			COALESCE(NULLIF(TRIM(connections.name), ''), 'connection-' || connections.id::text),
			COALESCE(src.openai_accepted_format, ''),
			COALESCE(connections.openai_text_capability, '')
		FROM model_access_targets mat
		JOIN model_configs src ON src.id = mat.source_model_config_id
		JOIN connections ON connections.id = mat.target_connection_id
		WHERE mat.profile_id = $1
			AND src.api_family = 'openai'
			AND src.openai_accepted_format IS DISTINCT FROM connections.openai_text_capability
		ORDER BY src.model_id ASC, connections.id ASC`, profileID)
	if err != nil {
		return Report{}, fmt.Errorf("scan openai connection-target mode violations: %w", err)
	}
	for rows.Next() {
		var violation Violation
		if err := rows.Scan(&violation.SourceModelID, &violation.TargetID, &violation.SourceMode, &violation.TargetMode); err != nil {
			rows.Close()
			return Report{}, fmt.Errorf("scan openai connection-target violation row: %w", err)
		}
		violation.RelationKind = RelationKindConnectionTarget
		report.Violations = append(report.Violations, violation)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Report{}, fmt.Errorf("iterate openai connection-target violations: %w", err)
	}
	rows.Close()

	return report, nil
}
