package contracttest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// The contracts below cover the /route/pricing source-linked import closure:
// discovery stays automatic but never auto-selects, a preview must carry every
// price component plus its own source evidence, a template-only commit writes
// no Terminal Target rows, one conflicting target rolls the whole batch back,
// and a soft-deleted source template can be re-imported as a new live template
// without losing its history.

const (
	catalogPreviewPath = "/api/pricing-templates/catalog/preview"
	catalogCommitPath  = "/api/pricing-templates/catalog/commit"
)

// catalogImport keeps a preview together with the request that produced it, so
// a commit can replay the exact offering-resolution inputs the operator saw.
// The offering is addressed either by a persisted binding (model_config_id
// alone) or by explicit coordinates, and the hash covers which one was used.
type catalogImport struct {
	request map[string]any
	preview map[string]any
}

// catalogCreateTerminalTarget creates one Terminal Target owned by the model
// and returns its connection id.
func catalogCreateTerminalTarget(t *testing.T, harness *contractHarness, modelConfigID int, name string) int {
	t.Helper()
	body := map[string]any{
		"endpoint_create":        map[string]any{"name": name + " Endpoint", "base_url": "https://pricing-import.example/v1", "api_key": "sk-pricing-import"},
		"name":                   name,
		"is_active":              true,
		"openai_text_capability": "dual_native",
	}
	connection := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), body, nil, http.StatusCreated)["connection"].(map[string]any)
	return jsonInt(t, connection["id"])
}

// catalogPreview runs one catalog pricing preview and keeps its request so the
// matching commit replays the same coordinates.
// catalogAssertCount runs a zero-argument count query.
func catalogAssertCount(t *testing.T, harness *contractHarness, query string, want int) {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), query).Scan(&count); err != nil {
		t.Fatalf("count rows (%s): %v", query, err)
	}
	if count != want {
		t.Fatalf("expected %d rows for %s, got %d", want, query, count)
	}
}

// catalogBind binds a model to explicit offering coordinates so a test can
// exercise the model-detail preview path, which resolves the offering from the
// persisted binding instead of the request.
func catalogBind(t *testing.T, harness *contractHarness, modelConfigID int, providerID, modelID, revision string) {
	t.Helper()
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/bind", modelConfigID), map[string]any{
		"provider_id": providerID, "catalog_model_id": modelID, "expected_catalog_revision": revision,
	}, nil, http.StatusOK)
}

func catalogPreview(t *testing.T, harness *contractHarness, request map[string]any) catalogImport {
	t.Helper()
	preview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPreviewPath, request, nil, http.StatusOK)
	return catalogImport{request: request, preview: preview}
}

func (imported catalogImport) body(connectionIDs []int, overrides map[string]any) map[string]any {
	body := map[string]any{
		"schema_version":            imported.preview["schema_version"],
		"connection_ids":            connectionIDs,
		"preview_hash":              imported.preview["preview_hash"],
		"expected_catalog_revision": imported.preview["catalog_revision"],
	}
	for _, key := range []string{"model_config_id", "provider_id", "catalog_model_id"} {
		if value, ok := imported.request[key]; ok {
			body[key] = value
		}
	}
	for key, value := range overrides {
		body[key] = value
	}
	return body
}

// commit posts the replay and returns the raw response so a test can assert a
// rejection status.
func (imported catalogImport) commit(t *testing.T, harness *contractHarness, connectionIDs []int, overrides map[string]any) *http.Response {
	t.Helper()
	return harness.requestJSON(t, harness.client, http.MethodPost, catalogCommitPath, imported.body(connectionIDs, overrides), nil)
}

// commitOK posts the replay and requires success.
func (imported catalogImport) commitOK(t *testing.T, harness *contractHarness, connectionIDs []int, overrides map[string]any) map[string]any {
	t.Helper()
	response := imported.commit(t, harness, connectionIDs, overrides)
	assertStatus(t, response, http.StatusOK)
	return decodeJSONMap(t, response)
}

func catalogPlanCards(t *testing.T, preview map[string]any, role string) map[string]any {
	t.Helper()
	plan := asMap(t, preview["plan"])
	cards := asMap(t, plan["cards"])
	card, ok := cards[role].(map[string]any)
	if !ok {
		t.Fatalf("preview plan is missing the %q card: %+v", role, cards)
	}
	return card
}

type catalogAtomicState struct {
	CurrentRevisionID   int64
	RevisionCount       int
	TemplateGeneration  int64
	ReferenceGeneration int64
	RuntimeGeneration   int64
	ReservationCount    int
	OperationCount      int
	ResultItemCount     int
	TargetTemplateID    int
	TargetUpdatedAt     string
}

// catalogLoadAtomicState captures every durable surface that the drift update
// opens before Terminal Target assignment. Keeping this one comparable record
// makes the post-conflict assertion prove the whole transaction rolled back.
func catalogLoadAtomicState(t *testing.T, harness *contractHarness, profileID, templateID, targetID int) catalogAtomicState {
	t.Helper()
	var state catalogAtomicState
	err := harness.conn.QueryRow(context.Background(), `
		SELECT templates.current_revision_id,
		       (SELECT COUNT(*) FROM pricing_template_revisions WHERE template_id = templates.id),
		       settings.pricing_template_generation,
		       settings.pricing_reference_generation,
		       (SELECT COALESCE(SUM(version), 0) FROM runtime_cache_generations),
		       (SELECT COUNT(*) FROM pricing_mutation_operation_reservations WHERE profile_id = settings.profile_id),
		       (SELECT COUNT(*) FROM pricing_mutation_operations WHERE profile_id = settings.profile_id),
		       (SELECT COUNT(*) FROM pricing_mutation_result_items AS items JOIN pricing_mutation_operations AS operations ON operations.operation_id = items.operation_id WHERE operations.profile_id = settings.profile_id),
		       targets.pricing_template_id,
		       targets.updated_at::text
		  FROM pricing_templates AS templates
		  JOIN user_settings AS settings ON settings.profile_id = templates.profile_id
		  JOIN connections AS targets ON targets.id = $3 AND targets.profile_id = templates.profile_id
		 WHERE templates.profile_id = $1 AND templates.id = $2`, profileID, templateID, targetID).Scan(
		&state.CurrentRevisionID,
		&state.RevisionCount,
		&state.TemplateGeneration,
		&state.ReferenceGeneration,
		&state.RuntimeGeneration,
		&state.ReservationCount,
		&state.OperationCount,
		&state.ResultItemCount,
		&state.TargetTemplateID,
		&state.TargetUpdatedAt,
	)
	if err != nil {
		t.Fatalf("load catalog atomic state: %v", err)
	}
	return state
}

// catalogWaitForConnectionRowLock waits until the catalog commit owns the
// earlier target row. The later target is held by the test transaction, so
// observing this lock proves the commit has already opened its drift revision
// and reached the assignment phase before that later target can conflict.
func catalogWaitForConnectionRowLock(t *testing.T, harness *contractHarness, connectionID int) {
	t.Helper()
	probeContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for probeContext.Err() == nil {
		var lockedID int
		err := harness.conn.QueryRow(probeContext, `SELECT id FROM connections WHERE id = $1 FOR UPDATE NOWAIT`, connectionID).Scan(&lockedID)
		var postgresErr *pgconn.PgError
		if errors.As(err, &postgresErr) && postgresErr.Code == "55P03" {
			return
		}
		if err != nil {
			t.Fatalf("probe Terminal Target %d row lock: %v", connectionID, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("catalog commit did not lock Terminal Target %d before the deadline", connectionID)
}

// TestCatalogPricingPreviewCarriesFullSourceEvidence covers C3: the preview must
// name both ends of the mapping, state the fixed USD/PER_1M unit, expose all
// five components with explicit 0 kept distinct from unconfigured null, and
// carry the catalog revision and fetch stamp. It also pins the uncacheable
// header contract on both catalog routes.
func TestCatalogPricingPreviewCarriesFullSourceEvidence(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Pricing Evidence Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-five-part")
	modelConfigID := jsonInt(t, model["id"])

	// C1: an unbound model still resolves a unique exact match, and the match
	// is only ever reported — nothing is written or auto-selected.
	matchPath := fmt.Sprintf("/api/models/%d/catalog/match-preview", modelConfigID)
	match := requestJSONStatus[map[string]any](t, harness, http.MethodPost, matchPath, map[string]any{}, nil, http.StatusOK)
	if match["reason"] != "unique_match" || match["committable"] != true {
		t.Fatalf("unique exact match must be committable: %+v", match)
	}
	if match["provider_id"] != "openai" || match["catalog_model_id"] != "gpt-five-part" {
		t.Fatalf("match coordinates drifted: %+v", match)
	}

	// C2: the pricing-page flow previews with no Terminal Target at all.
	imported := catalogPreview(t, harness, map[string]any{
		"model_config_id":  modelConfigID,
		"provider_id":      match["provider_id"],
		"catalog_model_id": match["catalog_model_id"],
		"connection_ids":   []int{},
	})
	preview := imported.preview

	if asMap(t, preview["model"])["model_config_id"].(float64) != float64(modelConfigID) {
		t.Fatalf("preview must name the Prism model: %+v", preview["model"])
	}
	if asMap(t, preview["model"])["model_id"] != "gpt-five-part" {
		t.Fatalf("preview Prism model id drifted: %+v", preview["model"])
	}
	if preview["catalog_currency"] != "USD" || preview["pricing_unit"] != "PER_1M" {
		t.Fatalf("preview must state the fixed USD/PER_1M unit: %+v", preview)
	}
	if preview["reporting_currency_code"] != "USD" {
		t.Fatalf("reporting currency must be visible: %+v", preview)
	}
	if revision, ok := preview["catalog_revision"].(string); !ok || revision == "" {
		t.Fatalf("preview must carry the catalog revision: %+v", preview)
	}
	fetchedAt, ok := preview["fetched_at"].(string)
	if !ok {
		t.Fatalf("preview must carry the catalog fetch stamp: %+v", preview)
	}
	if _, err := time.Parse(time.RFC3339, fetchedAt); err != nil {
		t.Fatalf("fetched_at must be RFC3339: %v", err)
	}
	if preview["action"] != "create" || preview["committable"] != true {
		t.Fatalf("first import must offer a template creation: %+v", preview)
	}
	if targets := preview["targets"].([]any); len(targets) != 0 {
		t.Fatalf("pricing-page preview must carry zero targets: %+v", targets)
	}

	card := catalogPlanCards(t, preview, "standard")
	if card["input_price"] != "1.25" || card["output_price"] != "10" {
		t.Fatalf("base prices drifted: %+v", card)
	}
	// An explicit catalog zero stays "0"; it must never collapse to null.
	if card["cached_input_price"] != "0" {
		t.Fatalf("explicit zero cache read must stay \"0\": %+v", card)
	}
	if card["cache_creation_price"] != "1.5" {
		t.Fatalf("cache write drifted: %+v", card)
	}
	// The fifth component is part of the preview contract.
	if card["reasoning_price"] != "12.5" {
		t.Fatalf("reasoning price must be previewed: %+v", card)
	}

	// A tiered offering surfaces its threshold verbatim, and an offering with
	// no specialty components reports them as null rather than zero.
	tierImported := catalogPreview(t, harness, map[string]any{
		"model_config_id": modelConfigID, "provider_id": "openai", "catalog_model_id": "gpt-long", "connection_ids": []int{},
	})
	tierPlan := asMap(t, tierImported.preview["plan"])
	if tierPlan["template_kind"] != "tiered" {
		t.Fatalf("single context tier must preview as tiered: %+v", tierPlan)
	}
	if threshold, ok := tierPlan["tier_input_tokens_above"].(float64); !ok || threshold != 272000 {
		t.Fatalf("tier threshold must appear verbatim: %v", tierPlan["tier_input_tokens_above"])
	}
	tierCard := catalogPlanCards(t, tierImported.preview, "tier_base")
	if tierCard["reasoning_price"] != nil {
		t.Fatalf("missing reasoning price must stay null: %+v", tierCard)
	}
	if tierCard["cached_input_price"] != nil {
		t.Fatalf("missing cache read must stay null: %+v", tierCard)
	}

	// Both catalog routes must answer uncacheable: a cached preview is a stale
	// write authorization.
	for _, path := range []string{catalogPreviewPath, catalogCommitPath} {
		response := modelResponse(t, harness, profileID, http.MethodPost, path, map[string]any{"connection_ids": []int{}})
		if got := response.Header.Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("%s Cache-Control = %q, want %q", path, got, "private, no-store")
		}
		for _, want := range []string{"Authorization", "Cookie", "X-Profile-Id"} {
			if !strings.Contains(response.Header.Get("Vary"), want) {
				t.Fatalf("%s Vary = %q, want it to cover %s", path, response.Header.Get("Vary"), want)
			}
		}
	}
}

// TestCatalogPricingManualCandidateSelectionAndTemplateOnlyCommit covers C1's
// zero-match branch and C2's template-only default: a model whose offering only
// an unmapped provider carries can never auto-match, so a human searches the
// bounded candidate list, picks coordinates, and commits with no target.
func TestCatalogPricingManualCandidateSelectionAndTemplateOnlyCommit(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Pricing Manual Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-5.6-luna")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	// The api_family mapping only knows the upstream-native provider, so this
	// coordinate is a genuine no-match rather than an ambiguity.
	match := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	if match["reason"] != "no_match" || match["committable"] != false {
		t.Fatalf("unmapped provider must not auto-match: %+v", match)
	}
	if candidates := match["candidates"].([]any); len(candidates) != 0 {
		t.Fatalf("no_match must offer zero auto candidates: %+v", candidates)
	}

	// A human searches the whole catalog and picks the coordinate explicitly.
	search := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath+"/candidates?scope=all&q=luna&limit=20", nil, nil, http.StatusOK)
	items := search["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("bounded search must return exactly the one luna offering: %+v", items)
	}
	picked := asMap(t, items[0])
	if picked["provider_id"] != "codex" || picked["model_id"] != "gpt-5.6-luna" {
		t.Fatalf("candidate coordinates drifted: %+v", picked)
	}
	// The family-scoped search must not reach the unmapped provider at all.
	familySearch := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath+"/candidates?scope=family&q=luna&limit=20", nil, nil, http.StatusOK)
	if len(familySearch["items"].([]any)) != 0 {
		t.Fatalf("family scope must stay inside the mapped providers: %+v", familySearch)
	}

	// Preview on the manually chosen coordinates, with no target selected.
	imported := catalogPreview(t, harness, map[string]any{
		"model_config_id":  modelConfigID,
		"provider_id":      "codex",
		"catalog_model_id": "gpt-5.6-luna",
		"connection_ids":   []int{},
	})
	preview := imported.preview
	if preview["committable"] != true || preview["action"] != "create" {
		t.Fatalf("manual coordinates must preview committable: %+v", preview)
	}
	manualCard := catalogPlanCards(t, preview, "standard")
	if manualCard["cached_input_price"] != "0.05" {
		t.Fatalf("manual coordinates must project the offering prices: %+v", manualCard)
	}

	// Commit with zero targets: the template and its import revision land, and
	// no Terminal Target is touched.
	committed := imported.commitOK(t, harness, []int{}, nil)
	if committed["created"] != true || committed["updated"] != false {
		t.Fatalf("template-only commit must create exactly one template: %+v", committed)
	}
	if assigned := committed["assigned_connection_ids"].([]any); len(assigned) != 0 {
		t.Fatalf("template-only commit must assign zero targets: %+v", assigned)
	}
	if committed["template_name"] != "codex/gpt-5.6-luna" {
		t.Fatalf("commit must return the authoritative stored template name: %+v", committed)
	}
	templateID := jsonInt(t, committed["template_id"])
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM connections WHERE pricing_template_id = $1`, templateID, 0)

	// Importing prices never bound the model as a side effect.
	catalogAssertCount(t, harness, fmt.Sprintf(`SELECT COUNT(*) FROM model_catalog_bindings WHERE model_config_id = %d`, modelConfigID), 0)

	// The template read surface traces back to the catalog.
	template := requestJSONStatus[map[string]any](t, harness, http.MethodGet, fmt.Sprintf("/api/pricing-templates/%d", templateID), nil, nil, http.StatusOK)
	if template["catalog_provider_id"] != "codex" || template["catalog_model_id"] != "gpt-5.6-luna" {
		t.Fatalf("template must expose its catalog coordinates: %+v", template)
	}
	if template["name"] != committed["template_name"] {
		t.Fatalf("commit template_name must match the persisted read model: commit=%+v template=%+v", committed, template)
	}
	if template["revision_source"] != "catalog" || template["catalog_revision"] != preview["catalog_revision"] {
		t.Fatalf("template must expose revision provenance: %+v", template)
	}
	revisions := requestJSONStatus[[]any](t, harness, http.MethodGet, fmt.Sprintf("/api/pricing-templates/%d/revisions", templateID), nil, nil, http.StatusOK)
	if len(revisions) != 1 {
		t.Fatalf("template-only commit must create one revision: %+v", revisions)
	}
	revision := asMap(t, revisions[0])
	if revision["revision_source"] != "catalog" || revision["catalog_revision"] != preview["catalog_revision"] {
		t.Fatalf("revision must expose catalog provenance: %+v", revision)
	}

	// A re-import of the unchanged offering reuses the same live template.
	repreview := catalogPreview(t, harness, map[string]any{
		"model_config_id": modelConfigID, "provider_id": "codex", "catalog_model_id": "gpt-5.6-luna", "connection_ids": []int{},
	})
	if repreview.preview["action"] != "reuse" || asMap(t, repreview.preview["template"])["id"].(float64) != float64(templateID) {
		t.Fatalf("unchanged re-import must reuse the linked template: %+v", repreview.preview)
	}
	repreview.commitOK(t, harness, []int{}, nil)
	catalogAssertCount(t, harness, `SELECT COUNT(*) FROM pricing_templates WHERE catalog_provider_id = 'codex' AND catalog_model_id = 'gpt-5.6-luna' AND deleted_at IS NULL`, 1)
}

// TestCatalogPricingMultiTargetSuccessAndSingleTargetRollback covers C4: the
// targets of one commit are atomic, so a conflict on any single one of them
// leaves the template, its revisions, and every other target unchanged.
func TestCatalogPricingMultiTargetSuccessAndSingleTargetRollback(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Pricing Atomic Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])

	firstTarget := catalogCreateTerminalTarget(t, harness, modelConfigID, "Pricing Target A")
	secondTarget := catalogCreateTerminalTarget(t, harness, modelConfigID, "Pricing Target B")
	if firstTarget >= secondTarget {
		t.Fatalf("fixture must create targets in ascending id order: %d >= %d", firstTarget, secondTarget)
	}
	// Requests may arrive in any order, but the protocol locks the normalized
	// ascending order. The test holds the later row so the earlier row is
	// necessarily written before the later CAS check fails.
	targets := []int{secondTarget, firstTarget}

	// This is the model-detail path: the offering comes from the persisted
	// binding, not from request coordinates.
	match := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/match-preview", modelConfigID), map[string]any{}, nil, http.StatusOK)
	catalogBind(t, harness, modelConfigID, "openai", "gpt-contract", match["catalog_revision"].(string))

	imported := catalogPreview(t, harness, map[string]any{"model_config_id": modelConfigID, "connection_ids": targets})
	if len(imported.preview["targets"].([]any)) != 2 {
		t.Fatalf("preview must echo both targets: %+v", imported.preview["targets"])
	}

	committed := imported.commitOK(t, harness, targets, nil)
	if assigned := committed["assigned_connection_ids"].([]any); len(assigned) != 2 {
		t.Fatalf("both targets must be assigned in one commit: %+v", committed)
	}
	templateID := jsonInt(t, committed["template_id"])
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM connections WHERE pricing_template_id = $1`, templateID, 2)

	// Create real source drift. A confirmed replay would append a catalog
	// revision and advance the template generation before assigning targets.
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", templateID), map[string]any{
		"card": map[string]any{
			"input_price": "99", "output_price": "199", "cached_input_price": "0",
			"cache_creation_price": nil, "reasoning_price": nil,
		},
	}, nil, http.StatusOK)
	moved := catalogPreview(t, harness, map[string]any{"model_config_id": modelConfigID, "connection_ids": targets})
	if moved.preview["action"] != "drift" || moved.preview["drift"] != true {
		t.Fatalf("manual price edit must open a drift revision: %+v", moved.preview)
	}
	before := catalogLoadAtomicState(t, harness, profileID, templateID, firstTarget)

	// Hold and change the later target without committing. The catalog commit's
	// ordinary reads still see the previewed row, so the hash matches; after it
	// writes the drift revision and earlier target it blocks on this row.
	blockerContext, cancelBlocker := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelBlocker()
	blockerConnection := connectDatabase(t, blockerContext, harness.dsn)
	defer func() { _ = blockerConnection.Close(context.Background()) }()
	blocker, err := blockerConnection.Begin(blockerContext)
	if err != nil {
		t.Fatalf("begin later-target blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(blockerContext, `UPDATE connections SET updated_at = updated_at + INTERVAL '1 second' WHERE id = $1`, secondTarget); err != nil {
		t.Fatalf("move later target under row lock: %v", err)
	}

	rawCommit, err := json.Marshal(moved.body(targets, map[string]any{"confirm_drift": true}))
	if err != nil {
		t.Fatalf("marshal concurrent catalog commit: %v", err)
	}
	commitRequest, err := http.NewRequest(http.MethodPost, harness.url+catalogCommitPath, bytes.NewReader(rawCommit))
	if err != nil {
		t.Fatalf("build concurrent catalog commit: %v", err)
	}
	commitRequest.Header.Set("Content-Type", "application/json")
	type commitResult struct {
		response *http.Response
		err      error
	}
	commitDone := make(chan commitResult, 1)
	go func() {
		response, requestErr := harness.client.Do(commitRequest)
		commitDone <- commitResult{response: response, err: requestErr}
	}()

	catalogWaitForConnectionRowLock(t, harness, firstTarget)
	if err := blocker.Commit(blockerContext); err != nil {
		t.Fatalf("publish later-target conflict: %v", err)
	}

	var result commitResult
	select {
	case result = <-commitDone:
	case <-time.After(10 * time.Second):
		t.Fatal("catalog commit did not finish after releasing the later target")
	}
	if result.err != nil {
		t.Fatalf("concurrent catalog commit: %v", result.err)
	}
	assertStatus(t, result.response, http.StatusConflict)
	conflict := decodeJSONMap(t, result.response)
	if conflict["pricing_cas_conflict"] != true || jsonInt(t, conflict["connection_id"]) != secondTarget {
		t.Fatalf("later target must fail the assignment CAS: %+v", conflict)
	}
	if detail, ok := conflict["detail"].(string); !ok || !strings.Contains(detail, "changed since the preview") {
		t.Fatalf("unexpected assignment conflict detail: %+v", conflict)
	}

	after := catalogLoadAtomicState(t, harness, profileID, templateID, firstTarget)
	if after != before {
		t.Fatalf("catalog conflict must roll back revision, current pointer, generations, ledgers, and earlier target:\nbefore=%+v\nafter=%+v", before, after)
	}
}

// TestCatalogPricingDeletedSourceTemplateReimport covers C5: soft-deleting a
// source-linked template releases its offering coordinates for a fresh live
// template while every retained row survives.
func TestCatalogPricingDeletedSourceTemplateReimport(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Pricing Reimport Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	match := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/match-preview", modelConfigID), map[string]any{}, nil, http.StatusOK)
	catalogBind(t, harness, modelConfigID, "openai", "gpt-contract", match["catalog_revision"].(string))

	imported := catalogPreview(t, harness, map[string]any{"model_config_id": modelConfigID, "connection_ids": []int{}})
	committed := imported.commitOK(t, harness, []int{}, nil)
	originalTemplateID := jsonInt(t, committed["template_id"])

	var originalRevisions int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM pricing_template_revisions WHERE template_id = $1`, originalTemplateID).Scan(&originalRevisions); err != nil {
		t.Fatalf("count original revisions: %v", err)
	}
	if originalRevisions != 1 {
		t.Fatalf("original template must carry its import revision: %d", originalRevisions)
	}

	requestJSONStatus[map[string]any](t, harness, http.MethodDelete, fmt.Sprintf("/api/pricing-templates/%d", originalTemplateID), nil, nil, http.StatusOK)

	// The retired row keeps its coordinates as provenance and stays deleted.
	var deletedProvider, deletedModel string
	var deletedAt *time.Time
	if err := harness.conn.QueryRow(context.Background(),
		`SELECT catalog_provider_id, catalog_model_id, deleted_at FROM pricing_templates WHERE id = $1`, originalTemplateID).Scan(&deletedProvider, &deletedModel, &deletedAt); err != nil {
		t.Fatalf("load retired template: %v", err)
	}
	if deletedProvider != "openai" || deletedModel != "gpt-contract" || deletedAt == nil {
		t.Fatalf("retired template provenance changed: %q/%q/%v", deletedProvider, deletedModel, deletedAt)
	}

	// The same offering previews as a fresh creation instead of colliding.
	reimport := catalogPreview(t, harness, map[string]any{
		"model_config_id": modelConfigID, "provider_id": "openai", "catalog_model_id": "gpt-contract", "connection_ids": []int{},
	})
	if reimport.preview["action"] != "create" {
		t.Fatalf("deleted source template must free its coordinates: %+v", reimport.preview)
	}
	reimported := reimport.commitOK(t, harness, []int{}, nil)
	newTemplateID := jsonInt(t, reimported["template_id"])
	if newTemplateID == originalTemplateID {
		t.Fatalf("re-import must create a new live template row")
	}

	// Exactly one live template claims the offering; the deleted one survives
	// with its full revision history.
	catalogAssertCount(t, harness, `SELECT COUNT(*) FROM pricing_templates WHERE catalog_provider_id = 'openai' AND catalog_model_id = 'gpt-contract' AND deleted_at IS NULL`, 1)
	var retainedRevisions int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM pricing_template_revisions WHERE template_id = $1`, originalTemplateID).Scan(&retainedRevisions); err != nil {
		t.Fatalf("count retained revisions: %v", err)
	}
	if retainedRevisions != originalRevisions {
		t.Fatalf("re-import rewrote retained history: before=%d after=%d", originalRevisions, retainedRevisions)
	}

	// The new live template reads back with its own coordinates and evidence.
	latest := requestJSONStatus[map[string]any](t, harness, http.MethodGet, fmt.Sprintf("/api/pricing-templates/%d", newTemplateID), nil, nil, http.StatusOK)
	if latest["catalog_provider_id"] != "openai" || latest["catalog_model_id"] != "gpt-contract" || latest["revision_source"] != "catalog" {
		t.Fatalf("re-imported template lost its provenance: %+v", latest)
	}

	// The bounded list page carries the same evidence and attributes the
	// offering to the live row only.
	page := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/pricing-templates?limit=100", nil, nil, http.StatusOK)
	liveBySource := map[string]string{}
	for _, raw := range page["items"].([]any) {
		item := asMap(t, raw)
		provider, ok := item["catalog_provider_id"].(string)
		if !ok {
			continue
		}
		modelID, ok := item["catalog_model_id"].(string)
		if !ok {
			continue
		}
		liveBySource[provider+"/"+modelID] = item["id"].(string)
	}
	if liveBySource["openai/gpt-contract"] != fmt.Sprint(newTemplateID) {
		t.Fatalf("list page must attribute the offering to the live template: %+v", liveBySource)
	}
}
