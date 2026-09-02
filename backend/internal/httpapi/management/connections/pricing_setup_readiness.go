package connections

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/jackc/pgx/v5"
)

// SetupMatchingWitnessProjection is the stable matching projection returned
// by Pricing/Proxy setup-readiness reads: the canonical Model ref (same cut,
// name provenance included) plus the matching route witness. The coordinator
// never resolves names itself and never holds the full witness set.
type SetupMatchingWitnessProjection struct {
	Witness modelrouting.RouteWitnessRef `json:"witness"`
	Model   modelrouting.ModelEntityRef  `json:"model"`
}

// PricingSetupReadiness is the aggregate projection returned by
// GET /api/pricing-templates?include=setup_readiness&expected_route_witness_generation={G}.
type PricingSetupReadiness struct {
	EvaluatedRouteWitnessGeneration string                          `json:"evaluated_route_witness_generation"`
	PricingTemplateGeneration       int64                           `json:"pricing_template_generation"`
	PricingReferenceGeneration      int64                           `json:"pricing_reference_generation"`
	Configuration                   modelrouting.ReadinessAxis      `json:"configuration"`
	Application                     modelrouting.ReadinessAxis      `json:"application"`
	RouteWitnessCount               int                             `json:"route_witness_count"`
	AppliedWitnessCount             int                             `json:"applied_witness_count"`
	CostReadyWitnessCount           int                             `json:"cost_ready_witness_count"`
	CostReady                       *bool                           `json:"cost_ready"`
	RepresentativeMatching          *SetupMatchingWitnessProjection `json:"representative_matching"`
}

// pricingSetupReadinessState carries the per-witness pricing facts resolved
// in one batch (no per-witness queries).
type pricingSetupReadinessState struct {
	appliedTerminalTargetIDs   map[int]bool
	costReadyTerminalTargetIDs map[int]bool
}

// handlePricingTemplatesWithSetupReadiness implements the setup readiness
// projection on the pricing-templates list (Auth/Landing SPEC §8.2). It is a
// read-only projection: the expected generation binds one immutable analyzer
// snapshot, and any mismatch is a typed 409 that never falls back to a
// guessed readiness.
func (s *Service) buildPricingSetupReadiness(ctx context.Context, tx pgx.Tx, profileID int, expectedGeneration string) (PricingSetupReadiness, error) {
	return buildPricingSetupReadinessTx(ctx, tx, profileID, expectedGeneration)
}

func buildPricingSetupReadinessTx(ctx context.Context, tx pgx.Tx, profileID int, expectedGeneration string) (PricingSetupReadiness, error) {
	currentGeneration, err := loadRouteWitnessGenerationForConnections(ctx, tx, profileID)
	if err != nil {
		return PricingSetupReadiness{}, err
	}
	if !sameDecimalValue(expectedGeneration, currentGeneration) {
		return PricingSetupReadiness{}, &DomainError{
			StatusCode: http.StatusConflict,
			Detail:     "route_witness_generation_changed: route witness generation changed; re-select the readiness snapshot",
		}
	}
	templateGeneration, referenceGeneration, err := loadPricingGenerations(ctx, tx, profileID)
	if err != nil {
		return PricingSetupReadiness{}, err
	}
	graph, err := modelrouting.LoadRouteWitnessGraph(ctx, tx, profileID)
	if err != nil {
		return PricingSetupReadiness{}, err
	}
	snapshot := modelrouting.AnalyzeRouteWitnessSnapshotWithOperations(graph, currentGeneration, runtime.ModelBoundRouteWitnessOperations())
	state, err := resolvePricingSetupReadinessState(ctx, tx, profileID, snapshot)
	if err != nil {
		return PricingSetupReadiness{}, err
	}
	configuration := modelrouting.ReadinessAxis{State: "not_ready", ReasonCodes: []string{"no_complete_template"}}
	completeTemplate, err := hasCompleteActivePricingTemplate(ctx, tx, profileID)
	if err != nil {
		return PricingSetupReadiness{}, err
	}
	if completeTemplate {
		configuration = modelrouting.ReadinessAxis{State: "ready", ReasonCodes: []string{}}
	}
	application := modelrouting.ReadinessAxis{State: "not_ready", ReasonCodes: []string{"no_applied_template"}}
	appliedCount := 0
	costReadyCount := 0
	for _, witness := range snapshot.DirectWitnesses {
		if state.appliedTerminalTargetIDs[parseWitnessTerminalID(witness)] {
			appliedCount++
		}
		if state.costReadyTerminalTargetIDs[parseWitnessTerminalID(witness)] {
			costReadyCount++
		}
	}
	if appliedCount > 0 {
		application = modelrouting.ReadinessAxis{State: "ready", ReasonCodes: []string{}}
	}
	var costReady *bool
	if len(snapshot.DirectWitnesses) > 0 {
		// The aggregate is authoritative over the complete server-side
		// witness set: zero matching is false, any unknown/mismatch is null.
		if appliedCount == 0 {
			value := false
			costReady = &value
		} else if costReadyCount == appliedCount {
			value := true
			costReady = &value
		}
	}
	projection, err := resolvePricingMatchingProjection(ctx, tx, profileID, snapshot, state)
	if err != nil {
		return PricingSetupReadiness{}, err
	}
	return PricingSetupReadiness{
		EvaluatedRouteWitnessGeneration: fmt.Sprintf("%d", currentGeneration),
		PricingTemplateGeneration:       templateGeneration,
		PricingReferenceGeneration:      referenceGeneration,
		Configuration:                   configuration,
		Application:                     application,
		RouteWitnessCount:               snapshot.DirectRouteWitnessCount,
		AppliedWitnessCount:             appliedCount,
		CostReadyWitnessCount:           costReadyCount,
		CostReady:                       costReady,
		RepresentativeMatching:          projection,
	}, nil
}

// loadRouteWitnessGenerationForConnections reads (and lazily ensures) the
// per-profile route witness generation.
func loadRouteWitnessGenerationForConnections(ctx context.Context, exec queryExecutor, profileID int) (int, error) {
	if _, err := exec.Exec(
		ctx,
		`INSERT INTO route_witness_generations (profile_id, generation, updated_at)
		VALUES ($1, 1, $2) ON CONFLICT (profile_id) DO NOTHING`,
		profileID,
		timeNowForConnections(),
	); err != nil {
		return 0, fmt.Errorf("ensure route witness generation for profile %d: %w", profileID, err)
	}
	var generation int
	if err := exec.QueryRow(ctx, `SELECT generation FROM route_witness_generations WHERE profile_id = $1`, profileID).Scan(&generation); err != nil {
		return 0, fmt.Errorf("read route witness generation for profile %d: %w", profileID, err)
	}
	return generation, nil
}

func timeNowForConnections() time.Time { return time.Now().UTC() }

// loadPricingGenerations reads the Pricing owner's template/reference
// generations (the owner-local revision the 409 mismatch guards against).
func loadPricingGenerations(ctx context.Context, exec queryExecutor, profileID int) (int64, int64, error) {
	var templateGeneration int64
	var referenceGeneration int64
	if err := exec.QueryRow(ctx, `SELECT pricing_template_generation, pricing_reference_generation FROM user_settings WHERE profile_id = $1`, profileID).Scan(&templateGeneration, &referenceGeneration); err != nil {
		return 0, 0, fmt.Errorf("load pricing generations for profile %d: %w", profileID, err)
	}
	return templateGeneration, referenceGeneration, nil
}

// hasCompleteActivePricingTemplate reports whether at least one non-deleted
// template has a usable current revision. Base input/output prices are the
// readiness contract; specialty NULLs are valid unconfigured components and
// are reported by the bounded Pricing owner list instead.
func hasCompleteActivePricingTemplate(ctx context.Context, exec queryExecutor, profileID int) (bool, error) {
	var found bool
	if err := exec.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pricing_templates templates
		JOIN pricing_template_revisions revisions ON revisions.id = templates.current_revision_id
		WHERE templates.profile_id = $1 AND templates.deleted_at IS NULL
		AND revisions.template_kind IN ('standard','tiered','peak_valley')
		AND (revisions.template_kind <> 'peak_valley' OR (revisions.pricing_schedule_timezone IS NOT NULL AND revisions.pricing_schedule_digest IS NOT NULL AND EXISTS (SELECT 1 FROM pricing_template_windows windows WHERE windows.revision_id = revisions.id)))
		AND NOT EXISTS (
			SELECT 1 FROM unnest(CASE revisions.template_kind
				WHEN 'standard' THEN ARRAY['standard']::text[]
				WHEN 'tiered' THEN ARRAY['tier_base','tier_above']::text[]
				ELSE ARRAY['peak','offpeak']::text[] END) AS required(role)
			WHERE NOT EXISTS (
				SELECT 1 FROM pricing_template_cards cards
				WHERE cards.revision_id = revisions.id AND cards.card_role = required.role
				AND cards.input_price IS NOT NULL AND cards.output_price IS NOT NULL
			)
		)
	)`, profileID).Scan(&found); err != nil {
		return false, fmt.Errorf("check complete pricing template for profile %d: %w", profileID, err)
	}
	if !found {
		return false, nil
	}
	rows, err := exec.Query(ctx, `SELECT revisions.pricing_schedule_timezone FROM pricing_templates templates JOIN pricing_template_revisions revisions ON revisions.id = templates.current_revision_id WHERE templates.profile_id = $1 AND templates.deleted_at IS NULL AND revisions.template_kind = 'peak_valley'`, profileID)
	if err != nil {
		return false, fmt.Errorf("check pricing template schedule timezones for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var timezone string
		if err := rows.Scan(&timezone); err != nil {
			return false, err
		}
		if _, err := time.LoadLocation(strings.TrimSpace(timezone)); err != nil {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return true, nil
}

// resolvePricingSetupReadinessState batches the per-witness pricing facts:
// whether the witness's terminal connection references a pricing template
// (applied) and whether that reference is cost-ready (active template,
// current revision, current currency epoch, canonical base prices).
func resolvePricingSetupReadinessState(ctx context.Context, exec queryExecutor, profileID int, snapshot modelrouting.RouteWitnessSnapshot) (pricingSetupReadinessState, error) {
	state := pricingSetupReadinessState{
		appliedTerminalTargetIDs:   map[int]bool{},
		costReadyTerminalTargetIDs: map[int]bool{},
	}
	if len(snapshot.DirectWitnesses) == 0 {
		return state, nil
	}
	terminalIDs := map[int]bool{}
	for _, witness := range snapshot.DirectWitnesses {
		terminalIDs[parseWitnessTerminalID(witness)] = true
	}
	var currentEpoch *int
	if err := exec.QueryRow(ctx, `SELECT current_reporting_currency_epoch_id FROM user_settings WHERE profile_id = $1`, profileID).Scan(&currentEpoch); err != nil {
		if err == pgx.ErrNoRows {
			currentEpoch = nil
		} else {
			return state, fmt.Errorf("load current reporting currency epoch for profile %d: %w", profileID, err)
		}
	}
	terminalIDList := make([]int, 0, len(terminalIDs))
	for id := range terminalIDs {
		terminalIDList = append(terminalIDList, id)
	}
	rows, err := exec.Query(ctx, `SELECT c.id, c.pricing_template_id, t.current_revision_id, r.reporting_currency_epoch, r.template_kind, r.pricing_schedule_timezone, r.pricing_schedule_digest,
		EXISTS (SELECT 1 FROM pricing_template_windows windows WHERE windows.revision_id = r.id) AS has_schedule_windows,
		NOT EXISTS (
			SELECT 1 FROM unnest(CASE r.template_kind
				WHEN 'standard' THEN ARRAY['standard']::text[]
				WHEN 'tiered' THEN ARRAY['tier_base','tier_above']::text[]
				WHEN 'peak_valley' THEN ARRAY['peak','offpeak']::text[]
				ELSE ARRAY[]::text[] END) AS required(role)
			WHERE NOT EXISTS (
				SELECT 1 FROM pricing_template_cards cards
				WHERE cards.revision_id = r.id AND cards.card_role = required.role
				AND cards.input_price IS NOT NULL AND cards.output_price IS NOT NULL
			)
		) AS cards_ready
		FROM connections c
		LEFT JOIN pricing_templates t ON t.id = c.pricing_template_id AND t.deleted_at IS NULL
		LEFT JOIN pricing_template_revisions r ON r.id = t.current_revision_id
		WHERE c.profile_id = $1 AND c.id = ANY($2)`, profileID, terminalIDList)
	if err != nil {
		return state, fmt.Errorf("query witness connections pricing for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var connectionID int
		var pricingTemplateID, currentRevisionID *int
		var revisionEpoch *int
		var templateKind, scheduleTimezone, scheduleDigest *string
		var hasScheduleWindows, cardsReady bool
		if err := rows.Scan(&connectionID, &pricingTemplateID, &currentRevisionID, &revisionEpoch, &templateKind, &scheduleTimezone, &scheduleDigest, &hasScheduleWindows, &cardsReady); err != nil {
			return state, fmt.Errorf("scan witness connection pricing for profile %d: %w", profileID, err)
		}
		if pricingTemplateID == nil || currentRevisionID == nil {
			continue
		}
		state.appliedTerminalTargetIDs[connectionID] = true
		if isCostReadyPricingRow(currentEpoch, revisionEpoch, templateKind, cardsReady, scheduleTimezone, scheduleDigest, hasScheduleWindows) {
			state.costReadyTerminalTargetIDs[connectionID] = true
		}
	}
	if err := rows.Err(); err != nil {
		return state, fmt.Errorf("iterate witness connection pricing for profile %d: %w", profileID, err)
	}
	return state, nil
}

// isCostReadyPricingRow applies the Pricing SPEC cost-ready predicate: the
// reference is the current revision of an active template on the current
// currency epoch with canonical input/output prices. Optional specialty
// components may be NULL; usage that needs one is resolved as missing price
// data by the runtime pricing path.
func isCostReadyPricingRow(currentEpoch *int, revisionEpoch *int, templateKind *string, cardsReady bool, scheduleTimezone, scheduleDigest *string, hasScheduleWindows bool) bool {
	if currentEpoch == nil || revisionEpoch == nil || *revisionEpoch != *currentEpoch || templateKind == nil || strings.TrimSpace(*templateKind) == "" || !cardsReady {
		return false
	}
	kind := pricingkind.Kind(strings.TrimSpace(*templateKind))
	if !kind.Valid() {
		return false
	}
	if kind != pricingkind.PeakValley {
		return scheduleTimezone == nil && scheduleDigest == nil && !hasScheduleWindows
	}
	if scheduleTimezone == nil || strings.TrimSpace(*scheduleTimezone) == "" || scheduleDigest == nil || strings.TrimSpace(*scheduleDigest) == "" || !hasScheduleWindows {
		return false
	}
	_, err := time.LoadLocation(strings.TrimSpace(*scheduleTimezone))
	return err == nil
}

// resolvePricingMatchingProjection returns the stable matching projection:
// the first cost-ready witness, else nil. The Model ref is resolved from the
// witness's model identity in the same cut with name provenance.
func resolvePricingMatchingProjection(ctx context.Context, exec queryExecutor, profileID int, snapshot modelrouting.RouteWitnessSnapshot, state pricingSetupReadinessState) (*SetupMatchingWitnessProjection, error) {
	for _, witness := range snapshot.DirectWitnesses {
		if !state.costReadyTerminalTargetIDs[parseWitnessTerminalID(witness)] {
			continue
		}
		modelRef, err := loadModelEntityRef(ctx, exec, profileID, witness)
		if err != nil {
			return nil, err
		}
		witness.Generation = snapshot.GenerationLabel()
		return &SetupMatchingWitnessProjection{Witness: witness, Model: *modelRef}, nil
	}
	return nil, nil
}

func parseWitnessTerminalID(witness modelrouting.RouteWitnessRef) int {
	result := 0
	for _, c := range witness.TerminalTargetID {
		if c < '0' || c > '9' {
			break
		}
		result = result*10 + int(c-'0')
	}
	return result
}

// loadModelEntityRef resolves the canonical Model EntityRef for the witness
// model in the same read cut (deleted=false when the row exists).
func loadModelEntityRef(ctx context.Context, exec queryExecutor, profileID int, witness modelrouting.RouteWitnessRef) (*modelrouting.ModelEntityRef, error) {
	var displayName *string
	var modelID string
	modelConfigID := parseWitnessModelConfigID(witness)
	err := exec.QueryRow(ctx, `SELECT model_id, display_name FROM model_configs WHERE profile_id = $1 AND id = $2`, profileID, modelConfigID).Scan(&modelID, &displayName)
	if err != nil {
		return nil, fmt.Errorf("load model ref for witness %s: %w", witness.WitnessID, err)
	}
	deleted := false
	name := ""
	nameSource := "unavailable"
	if displayName != nil && strings.TrimSpace(*displayName) != "" {
		name = strings.TrimSpace(*displayName)
		nameSource = "current"
	} else {
		name = modelID
		nameSource = "current"
	}
	return &modelrouting.ModelEntityRef{
		Kind:          "model",
		ModelConfigID: fmt.Sprintf("%d", modelConfigID),
		ModelID:       modelID,
		Name:          name,
		NameSource:    nameSource,
		Deleted:       &deleted,
	}, nil
}

func parseWitnessModelConfigID(witness modelrouting.RouteWitnessRef) int {
	result := 0
	for _, c := range witness.ModelConfigID {
		if c < '0' || c > '9' {
			break
		}
		result = result*10 + int(c-'0')
	}
	return result
}

func sameDecimalValue(expected string, actual int) bool {
	return expected == fmt.Sprintf("%d", actual)
}
