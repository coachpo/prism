package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/jackc/pgx/v5"
)

func runtimeSnapshotDomainError(err error) error {
	if errors.Is(err, ErrPublishedRuntimeSnapshotUnavailable) {
		return &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "Runtime snapshot is unavailable. Retry later."}
	}
	if errors.Is(err, ErrRuntimeSnapshotRefreshRequired) {
		return &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "Runtime snapshot refresh is required. Retry later."}
	}
	return err
}

func loadRuntimeReportCurrencySnapshot(ctx context.Context, tx pgx.Tx, profileID int) (runtimeReportCurrencySnapshot, error) {
	var code string
	var symbol string
	var epoch sql.NullInt64
	err := tx.QueryRow(ctx, `SELECT user_settings.report_currency_code, user_settings.report_currency_symbol, reporting_currency_epochs.epoch
		FROM user_settings
		LEFT JOIN reporting_currency_epochs ON reporting_currency_epochs.id = user_settings.current_reporting_currency_epoch_id
		WHERE user_settings.profile_id = $1 ORDER BY user_settings.id ASC LIMIT 1`, profileID).Scan(&code, &symbol, &epoch)
	if err == nil {
		snapshot := runtimeReportCurrencySnapshot{Code: strings.TrimSpace(code), Symbol: strings.TrimSpace(symbol)}
		if epoch.Valid {
			snapshot.Epoch = int(epoch.Int64)
		}
		return snapshot, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}, nil
	}
	return runtimeReportCurrencySnapshot{}, fmt.Errorf("load runtime report currency for profile %d: %w", profileID, err)
}

func listEnabledHeaderBlocklistRules(ctx context.Context, tx pgx.Tx, profileID int) ([]headerBlocklistRule, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT match_type, pattern
		FROM header_blocklist_rules
		WHERE enabled = TRUE AND (is_system = TRUE OR profile_id = $1)
		ORDER BY is_system DESC, id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query header blocklist rules for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]headerBlocklistRule, 0)
	for rows.Next() {
		var item headerBlocklistRule
		if err := rows.Scan(&item.MatchType, &item.Pattern); err != nil {
			return nil, fmt.Errorf("scan header blocklist rule: %w", err)
		}
		item.MatchType = strings.ToLower(strings.TrimSpace(item.MatchType))
		item.Pattern = strings.ToLower(strings.TrimSpace(item.Pattern))
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate header blocklist rules for profile %d: %w", profileID, err)
	}
	return items, nil
}

func toConnectionOrderCandidates(connections []runtimeConnection) []loadbalance.ConnectionOrderCandidate {
	candidates := make([]loadbalance.ConnectionOrderCandidate, 0, len(connections))
	for _, connection := range connections {
		candidates = append(candidates, loadbalance.ConnectionOrderCandidate{ID: connection.ID, Priority: connection.Priority})
	}
	return candidates
}

func runtimeConnectionRefs(connections []runtimeConnection) []loadbalance.RuntimeConnectionRef {
	refs := make([]loadbalance.RuntimeConnectionRef, 0, len(connections))
	for _, connection := range connections {
		refs = append(refs, loadbalance.RuntimeConnectionRef{ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID})
	}
	return refs
}

func listEnabledModelsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[string]runtimeModelRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT model_configs.id, model_configs.profile_id, model_configs.api_family, model_configs.model_id,
			model_configs.loadbalance_strategy_id, model_configs.openai_accepted_format,
			model_configs.openai_image_operations,
			model_configs.created_at,
			COALESCE(audit_settings.audit_enabled, FALSE),
			COALESCE(audit_settings.audit_enabled, FALSE) AND COALESCE(audit_settings.audit_capture_bodies, FALSE)
		FROM model_configs
		LEFT JOIN profile_api_family_audit_settings AS audit_settings ON audit_settings.profile_id = model_configs.profile_id
			AND audit_settings.api_family = model_configs.api_family
		WHERE model_configs.profile_id = $1 AND model_configs.is_enabled = TRUE
		ORDER BY model_configs.model_id ASC, model_configs.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query enabled models for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[string]runtimeModelRecord)
	for rows.Next() {
		var strategyID sql.NullInt32
		var openAIAcceptedFormat sql.NullString
		var openAIImageOperations sql.NullString
		item := runtimeModelRecord{}
		if err := rows.Scan(&item.ID, &item.ProfileID, &item.APIFamily, &item.ModelID, &strategyID, &openAIAcceptedFormat, &openAIImageOperations, &item.CreatedAt, &item.AuditEnabled, &item.AuditCaptureBodies); err != nil {
			return nil, fmt.Errorf("scan enabled model for profile %d: %w", profileID, err)
		}
		if _, exists := items[item.ModelID]; exists {
			continue
		}
		if strategyID.Valid {
			resolved := int(strategyID.Int32)
			item.LoadbalanceStrategyID = &resolved
		}
		item.OpenAIAcceptedFormat = nullableString(openAIAcceptedFormat)
		item.OpenAIImageOperations = nullableString(openAIImageOperations)
		items[item.ModelID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled models for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listAccessTargetsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int][]runtimeAccessTargetRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT model_access_targets.id, model_access_targets.profile_id, model_access_targets.source_model_config_id,
			model_access_targets.target_type, model_access_targets.target_model_config_id, target_models.model_id,
			target_models.profile_id, target_models.api_family, COALESCE(target_models.is_enabled, FALSE),
			model_access_targets.target_connection_id, connections.profile_id, connections.api_family,
			connections.openai_text_capability, connections.openai_image_capability,
			model_access_targets.position, model_access_targets.is_enabled
		FROM model_access_targets
		JOIN model_configs AS source_models ON source_models.id = model_access_targets.source_model_config_id
		LEFT JOIN model_configs AS target_models ON target_models.id = model_access_targets.target_model_config_id
		LEFT JOIN connections ON connections.id = model_access_targets.target_connection_id
		WHERE model_access_targets.profile_id = $1
		ORDER BY model_access_targets.source_model_config_id ASC, model_access_targets.position ASC, model_access_targets.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query access targets for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int][]runtimeAccessTargetRecord)
	for rows.Next() {
		var targetModelConfigID sql.NullInt32
		var targetModelID sql.NullString
		var targetModelProfileID sql.NullInt32
		var targetModelAPIFamily sql.NullString
		var targetModelEnabled sql.NullBool
		var targetConnectionID sql.NullInt32
		var targetConnectionProfileID sql.NullInt32
		var targetConnectionAPIFamily sql.NullString
		var connectionOpenAITextCapability sql.NullString
		var connectionOpenAIImageCapability sql.NullString
		item := runtimeAccessTargetRecord{}
		if err := rows.Scan(
			&item.ID,
			&item.ProfileID,
			&item.SourceModelConfigID,
			&item.TargetType,
			&targetModelConfigID,
			&targetModelID,
			&targetModelProfileID,
			&targetModelAPIFamily,
			&targetModelEnabled,
			&targetConnectionID,
			&targetConnectionProfileID,
			&targetConnectionAPIFamily,
			&connectionOpenAITextCapability,
			&connectionOpenAIImageCapability,
			&item.Position,
			&item.IsEnabled,
		); err != nil {
			return nil, fmt.Errorf("scan access target for profile %d: %w", profileID, err)
		}
		item.TargetModelConfigID = nullableInt32(targetModelConfigID)
		item.TargetModelID = strings.TrimSpace(targetModelID.String)
		if targetModelProfileID.Valid {
			item.TargetModelProfileID = int(targetModelProfileID.Int32)
		}
		if targetModelAPIFamily.Valid {
			item.TargetModelAPIFamily = strings.TrimSpace(targetModelAPIFamily.String)
		}
		item.TargetModelEnabled = targetModelEnabled.Valid && targetModelEnabled.Bool
		item.TargetConnectionID = nullableInt32(targetConnectionID)
		if targetConnectionProfileID.Valid {
			item.TargetConnectionProfileID = int(targetConnectionProfileID.Int32)
		}
		if targetConnectionAPIFamily.Valid {
			item.TargetConnectionAPIFamily = strings.TrimSpace(targetConnectionAPIFamily.String)
		}
		item.ConnectionOpenAITextCapability = nullableString(connectionOpenAITextCapability)
		item.ConnectionOpenAIImageCapability = nullableString(connectionOpenAIImageCapability)
		items[item.SourceModelConfigID] = append(items[item.SourceModelConfigID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate access targets for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listRuntimeStrategiesForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int]loadbalance.RuntimeStrategy, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio,
			retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds
		FROM loadbalance_strategies
		WHERE profile_id = $1
		ORDER BY id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query runtime strategies for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int]loadbalance.RuntimeStrategy)
	for rows.Next() {
		var legacyStrategyType string
		var failureStatusCodes []int32
		item := loadbalance.RuntimeStrategy{}
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&legacyStrategyType,
			&failureStatusCodes,
			&item.BanMode,
			&item.RetryBaseDelayMS,
			&item.RetryBackoffMultiplier,
			&item.RetryJitterRatio,
			&item.RetryMaxDelayMS,
			&item.CycleRetryAttemptLimit,
			&item.BanCumulativeRetryAttemptThreshold,
			&item.BanDurationSeconds,
		); err != nil {
			return nil, fmt.Errorf("scan runtime strategy for profile %d: %w", profileID, err)
		}
		item.LegacyStrategyType = &legacyStrategyType
		item.FailureStatusCodes = intSliceFromInt32(failureStatusCodes)
		items[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime strategies for profile %d: %w", profileID, err)
	}
	return items, nil
}

func intSliceFromInt32(values []int32) []int {
	items := make([]int, 0, len(values))
	for _, value := range values {
		items = append(items, int(value))
	}
	return items
}
