package models

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/jackc/pgx/v5"
)

// exportModelRow is one model_configs row in export scope.
type exportModelRow struct {
	ID                    int
	ModelID               string
	APIFamily             string
	DisplayName           *string
	IsEnabled             bool
	OpenAIAcceptedFormat  *string
	OpenAIImageOperations *string
}

// exportTargetRow is one reachable Terminal Target of one root model with the
// normalized current price shape resolved through its pricing template.
type exportTargetRow struct {
	ModelConfigID        int
	TerminalTargetID     int
	HopPosition          int
	EndpointID           int
	EndpointName         string
	OpenAITextCapability *string
	Pricing              *modelexport.TargetPriceSnapshot
}

// loadExportSnapshot reads every Default-profile model plus its reachable
// Terminal Targets, endpoints, current pricing revisions, and catalog bindings
// inside one read-only transaction. The caller owns the tx boundary so the
// whole snapshot is consistent.
func loadExportSnapshot(ctx context.Context, tx pgx.Tx) ([]exportModelRow, []exportTargetRow, map[int]catalogBindingRecord, *modelrouting.DiagnosticsGraph, error) {
	models, err := loadExportModels(ctx, tx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	targets, err := loadExportTargets(ctx, tx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	bindings, err := loadExportCatalogBindings(ctx, tx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	graph, err := modelrouting.LoadRouteWitnessGraph(ctx, tx, 1)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return models, targets, bindings, graph, nil
}

func loadExportModels(ctx context.Context, exec queryExecutor) ([]exportModelRow, error) {
	rows, err := exec.Query(ctx, `
		SELECT id, model_id, api_family, display_name, is_enabled,
		       openai_accepted_format, openai_image_operations
		FROM model_configs
		WHERE profile_id = 1
		ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query export models: %w", err)
	}
	defer rows.Close()
	models := []exportModelRow{}
	for rows.Next() {
		var row exportModelRow
		if err := rows.Scan(&row.ID, &row.ModelID, &row.APIFamily, &row.DisplayName, &row.IsEnabled,
			&row.OpenAIAcceptedFormat, &row.OpenAIImageOperations); err != nil {
			return nil, fmt.Errorf("scan export model: %w", err)
		}
		models = append(models, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export models: %w", err)
	}
	return models, nil
}

// loadExportTargets walks enabled access-target chains to every reachable
// active Terminal Target and resolves its endpoint plus current price shape.
// Ordering follows the minimum access-target position along each chain, then
// terminal target id.
func loadExportTargets(ctx context.Context, exec pgx.Tx) ([]exportTargetRow, error) {
	rows, err := exec.Query(ctx, `
		WITH RECURSIVE terminal_reachability AS (
			SELECT mat.source_model_config_id AS root_model_config_id,
				mat.target_model_config_id AS next_model_config_id,
				mat.target_connection_id AS terminal_connection_id,
				ARRAY[mat.position] AS hop_positions,
				ARRAY[mat.source_model_config_id] || COALESCE(ARRAY[mat.target_model_config_id], ARRAY[]::integer[]) AS path
			FROM model_access_targets mat
			WHERE mat.profile_id = 1 AND mat.is_enabled = TRUE
				AND (mat.target_connection_id IS NOT NULL OR mat.target_model_config_id IS NOT NULL)
			UNION ALL
			SELECT tr.root_model_config_id,
				mat.target_model_config_id,
				mat.target_connection_id,
				tr.hop_positions || mat.position,
				tr.path || COALESCE(ARRAY[mat.target_model_config_id], ARRAY[]::integer[])
			FROM terminal_reachability tr
			JOIN model_access_targets mat ON mat.profile_id = 1 AND mat.source_model_config_id = tr.next_model_config_id
			WHERE tr.next_model_config_id IS NOT NULL AND mat.is_enabled = TRUE
				AND (mat.target_connection_id IS NOT NULL OR (mat.target_model_config_id IS NOT NULL AND NOT mat.target_model_config_id = ANY(tr.path)))
		),
		distinct_terminals AS (
			SELECT root_model_config_id, terminal_connection_id, MIN(hop_positions[1]) AS first_position
			FROM terminal_reachability
			WHERE terminal_connection_id IS NOT NULL
			GROUP BY root_model_config_id, terminal_connection_id
		),
		ordered_terminals AS (
			SELECT d.root_model_config_id, d.terminal_connection_id,
				ROW_NUMBER() OVER (PARTITION BY d.root_model_config_id ORDER BY d.first_position ASC, d.terminal_connection_id ASC) - 1 AS position_index
			FROM distinct_terminals d
		)
		SELECT o.root_model_config_id,
			c.id AS terminal_target_id,
			o.position_index,
			e.id AS endpoint_id,
			COALESCE(e.name, '') AS endpoint_name,
			COALESCE(c.openai_text_capability::text, '') AS text_capability,
			c.is_active,
			t.id AS pricing_template_id,
			COALESCE(r.template_kind, '') AS template_kind,
			COALESCE(r.pricing_unit, '') AS pricing_unit,
			COALESCE(r.currency_code, '') AS currency_code,
			r.tier_input_tokens_above,
			MAX(ca.input_price) FILTER (WHERE ca.card_role = 'standard') AS std_input,
			MAX(ca.output_price) FILTER (WHERE ca.card_role = 'standard') AS std_output,
			MAX(ca.cached_input_price) FILTER (WHERE ca.card_role = 'standard') AS std_cached,
			MAX(ca.cache_creation_price) FILTER (WHERE ca.card_role = 'standard') AS std_creation,
			MAX(ca.reasoning_price) FILTER (WHERE ca.card_role = 'standard') AS std_reasoning,
			MAX(ca.input_price) FILTER (WHERE ca.card_role = 'tier_base') AS base_input,
			MAX(ca.output_price) FILTER (WHERE ca.card_role = 'tier_base') AS base_output,
			MAX(ca.cached_input_price) FILTER (WHERE ca.card_role = 'tier_base') AS base_cached,
			MAX(ca.cache_creation_price) FILTER (WHERE ca.card_role = 'tier_base') AS base_creation,
			MAX(ca.reasoning_price) FILTER (WHERE ca.card_role = 'tier_base') AS base_reasoning,
			MAX(ca.input_price) FILTER (WHERE ca.card_role = 'tier_above') AS above_input,
			MAX(ca.output_price) FILTER (WHERE ca.card_role = 'tier_above') AS above_output,
			MAX(ca.cached_input_price) FILTER (WHERE ca.card_role = 'tier_above') AS above_cached,
			MAX(ca.cache_creation_price) FILTER (WHERE ca.card_role = 'tier_above') AS above_creation,
			MAX(ca.reasoning_price) FILTER (WHERE ca.card_role = 'tier_above') AS above_reasoning
		FROM ordered_terminals o
		JOIN connections c ON c.id = o.terminal_connection_id AND c.profile_id = 1 AND c.is_active = TRUE
		JOIN endpoints e ON e.id = c.endpoint_id AND e.profile_id = 1
		LEFT JOIN pricing_templates t ON t.id = c.pricing_template_id AND t.deleted_at IS NULL
		LEFT JOIN pricing_template_revisions r ON r.id = t.current_revision_id
		LEFT JOIN pricing_template_cards ca ON ca.revision_id = r.id
		GROUP BY o.root_model_config_id, c.id, o.position_index, e.id, e.name,
			c.openai_text_capability, c.is_active, t.id, COALESCE(r.template_kind, ''),
			r.pricing_unit, r.currency_code, r.tier_input_tokens_above
		ORDER BY o.root_model_config_id ASC, o.position_index ASC, c.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query export reachable targets: %w", err)
	}
	defer rows.Close()
	targets := []exportTargetRow{}
	for rows.Next() {
		var row exportTargetRow
		var textCapability string
		var kind, unit, currency string
		var templateID *int
		var tierThreshold *int32
		var stdInput, stdOutput, stdCached, stdCreation, stdReasoning *string
		var baseInput, baseOutput, baseCached, baseCreation, baseReasoning *string
		var aboveInput, aboveOutput, aboveCached, aboveCreation, aboveReasoning *string
		var isActive bool
		if err := rows.Scan(&row.ModelConfigID, &row.TerminalTargetID, &row.HopPosition,
			&row.EndpointID, &row.EndpointName, &textCapability, &isActive, &templateID, &kind, &unit, &currency, &tierThreshold,
			&stdInput, &stdOutput, &stdCached, &stdCreation, &stdReasoning,
			&baseInput, &baseOutput, &baseCached, &baseCreation, &baseReasoning,
			&aboveInput, &aboveOutput, &aboveCached, &aboveCreation, &aboveReasoning); err != nil {
			return nil, fmt.Errorf("scan export target: %w", err)
		}
		if strings.TrimSpace(textCapability) != "" {
			row.OpenAITextCapability = &textCapability
		}
		if templateID != nil && strings.TrimSpace(kind) != "" {
			snapshot := buildTargetPriceSnapshot(kind, unit, currency, tierThreshold,
				[5]*string{stdInput, stdOutput, stdCached, stdCreation, stdReasoning},
				[5]*string{baseInput, baseOutput, baseCached, baseCreation, baseReasoning},
				[5]*string{aboveInput, aboveOutput, aboveCached, aboveCreation, aboveReasoning})
			row.Pricing = snapshot
			if row.Pricing != nil {
				row.Pricing.TerminalTargetID = row.TerminalTargetID
			}
		}
		targets = append(targets, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export targets: %w", err)
	}
	return targets, nil
}

// nullableCard assembles one card only when its input component exists;
// absent cards stay nil so "no template" and "unconfigured specialty" never
// collapse into each other.
func buildTargetPriceSnapshot(kind string, unit string, currency string, tierThreshold *int32,
	std [5]*string, base [5]*string, above [5]*string) *modelexport.TargetPriceSnapshot {
	snapshot := &modelexport.TargetPriceSnapshot{
		CurrencyCode: currency,
		PricingUnit:  unit,
	}
	switch kind {
	case string(pricingkind.Standard):
		snapshot.Kind = pricingkind.Standard
		snapshot.Card = cardFromParts(std)
	case string(pricingkind.Tiered):
		snapshot.Kind = pricingkind.Tiered
		snapshot.BaseCard = cardFromParts(base)
		snapshot.AboveCard = cardFromParts(above)
		if tierThreshold != nil {
			threshold := int(*tierThreshold)
			snapshot.TierThreshold = &threshold
		}
	case string(pricingkind.PeakValley):
		snapshot.Kind = pricingkind.PeakValley
	default:
		return nil
	}
	return snapshot
}

func cardFromParts(parts [5]*string) *modelexport.PriceCardSnapshot {
	if parts[0] == nil {
		return nil
	}
	return &modelexport.PriceCardSnapshot{
		InputPrice:         *parts[0],
		OutputPrice:        derefString(parts[1]),
		CachedInputPrice:   parts[2],
		CacheCreationPrice: parts[3],
		ReasoningPrice:     parts[4],
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// loadExportCatalogBindings reads every binding row for the profile in one
// query so source carries catalog evidence without per-model round trips.
func loadExportCatalogBindings(ctx context.Context, exec queryExecutor) (map[int]catalogBindingRecord, error) {
	rows, err := exec.Query(ctx,
		// catalogBindingSelectColumns already leads with bindings.model_config_id.
		`SELECT `+catalogBindingSelectColumns+`
		FROM model_catalog_bindings AS bindings
		JOIN model_configs AS configs ON configs.id = bindings.model_config_id
		WHERE configs.profile_id = 1`)
	if err != nil {
		return nil, fmt.Errorf("query export catalog bindings: %w", err)
	}
	defer rows.Close()
	bindings := map[int]catalogBindingRecord{}
	for rows.Next() {
		record := catalogBindingRecord{}
		var modalitiesInput, modalitiesOutput any
		var overrideModalitiesInput, overrideModalitiesOutput any
		if err := rows.Scan(
			&record.ModelConfigID, &record.ProviderID, &record.CatalogModelID, &record.MatchSource, &record.CatalogRevision, &record.FetchedAt, &record.UpdatedAt,
			&record.Source.Name, &record.Source.Description, &record.Source.Family, &record.Source.ReleaseDate, &record.Source.LastUpdated, &record.Source.Knowledge,
			&record.Source.Attachment, &record.Source.Reasoning, &record.Source.ToolCall, &record.Source.StructuredOutput, &record.Source.Temperature,
			&modalitiesInput, &modalitiesOutput, &record.Source.LimitContext, &record.Source.LimitInput, &record.Source.LimitOutput,
			&record.Source.OpenWeights, &record.Source.Status,
			&record.Override.Name, &record.Override.Description, &record.Override.Family, &record.Override.ReleaseDate, &record.Override.LastUpdated, &record.Override.Knowledge,
			&record.Override.Attachment, &record.Override.Reasoning, &record.Override.ToolCall, &record.Override.StructuredOutput, &record.Override.Temperature,
			&overrideModalitiesInput, &overrideModalitiesOutput, &record.Override.LimitContext, &record.Override.LimitInput, &record.Override.LimitOutput,
			&record.Override.OpenWeights, &record.Override.Status); err != nil {
			return nil, fmt.Errorf("scan export catalog binding: %w", err)
		}
		if record.Source.ModalitiesInput, err = decodeModalityColumn(modalitiesInput); err != nil {
			return nil, fmt.Errorf("decode export source modalities_input: %w", err)
		}
		if record.Source.ModalitiesOutput, err = decodeModalityColumn(modalitiesOutput); err != nil {
			return nil, fmt.Errorf("decode export source modalities_output: %w", err)
		}
		if record.Override.ModalitiesInput, err = decodeModalityColumn(overrideModalitiesInput); err != nil {
			return nil, fmt.Errorf("decode export override modalities_input: %w", err)
		}
		if record.Override.ModalitiesOutput, err = decodeModalityColumn(overrideModalitiesOutput); err != nil {
			return nil, fmt.Errorf("decode export override modalities_output: %w", err)
		}
		bindings[record.ModelConfigID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export catalog bindings: %w", err)
	}
	return bindings, nil
}

// sortTargetRowsByModel groups target rows per model preserving the SQL
// order and returns them keyed by model config id.
func sortTargetRowsByModel(rows []exportTargetRow) map[int][]exportTargetRow {
	grouped := map[int][]exportTargetRow{}
	for _, row := range rows {
		grouped[row.ModelConfigID] = append(grouped[row.ModelConfigID], row)
	}
	for modelID := range grouped {
		sort.SliceStable(grouped[modelID], func(i, j int) bool {
			left, right := grouped[modelID][i], grouped[modelID][j]
			if left.HopPosition != right.HopPosition {
				return left.HopPosition < right.HopPosition
			}
			return left.TerminalTargetID < right.TerminalTargetID
		})
	}
	return grouped
}
