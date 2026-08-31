package contracttest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The models.dev binding CAS contract: every drift scenario below is
// deterministic — the phases are separate synchronous requests (or an
// explicit transaction holding the row lock), never sleeps. Each test proves
// the rejection wrote nothing and the concurrent writer's facts survived.

func catalogBindAzureModel(t *testing.T, harness *contractHarness, modelConfigID int, revision string) map[string]any {
	t.Helper()
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)
	return requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/bind", catalogBindBody("gpt-contract", map[string]any{
		"provider_id": "azure", "catalog_model_id": "shared-model", "expected_catalog_revision": revision,
	}), nil, http.StatusOK)
}

type catalogAsyncHTTPResult struct {
	status int
	body   string
	err    error
}

// startCatalogJSONRequest builds the request on the test goroutine, then lets
// the HTTP writer run independently. The child never receives *testing.T and
// always publishes exactly one terminal result, so a handler failure cannot
// strand the coordinating test.
func startCatalogJSONRequest(t *testing.T, harness *contractHarness, method, path string, body any) <-chan catalogAsyncHTTPResult {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal async catalog request: %v", err)
	}
	request, err := http.NewRequest(method, harness.url+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("build async catalog request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	result := make(chan catalogAsyncHTTPResult, 1)
	go func() {
		response, requestErr := harness.client.Do(request)
		if requestErr != nil {
			result <- catalogAsyncHTTPResult{err: requestErr}
			return
		}
		responseBody, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil {
			result <- catalogAsyncHTTPResult{status: response.StatusCode, err: readErr}
			return
		}
		if closeErr != nil {
			result <- catalogAsyncHTTPResult{status: response.StatusCode, body: string(responseBody), err: closeErr}
			return
		}
		result <- catalogAsyncHTTPResult{status: response.StatusCode, body: string(responseBody)}
	}()
	return result
}

type catalogModelLockBarrier struct {
	armed       atomic.Bool
	reached     chan int
	release     chan struct{}
	releaseOnce sync.Once
}

func newCatalogModelLockBarrier() *catalogModelLockBarrier {
	return &catalogModelLockBarrier{
		reached: make(chan int, 1),
		release: make(chan struct{}),
	}
}

func (barrier *catalogModelLockBarrier) observe(modelConfigID int) {
	if barrier.armed.CompareAndSwap(true, false) {
		barrier.reached <- modelConfigID
		<-barrier.release
	}
}

func (barrier *catalogModelLockBarrier) arm(t *testing.T) {
	t.Helper()
	if !barrier.armed.CompareAndSwap(false, true) {
		t.Fatal("catalog model-lock barrier already armed")
	}
}

func (barrier *catalogModelLockBarrier) wait(t *testing.T, wantModelConfigID int) {
	t.Helper()
	select {
	case modelConfigID := <-barrier.reached:
		if modelConfigID != wantModelConfigID {
			t.Fatalf("catalog writer locked model %d, want %d", modelConfigID, wantModelConfigID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("HTTP catalog writer never reached the post-model-lock barrier")
	}
}

func (barrier *catalogModelLockBarrier) releaseWriter() {
	barrier.releaseOnce.Do(func() { close(barrier.release) })
}

func awaitCatalogJSONResult(t *testing.T, result <-chan catalogAsyncHTTPResult, wantStatus int) {
	t.Helper()
	select {
	case outcome := <-result:
		if outcome.err != nil {
			t.Fatalf("async catalog request failed: %v", outcome.err)
		}
		if outcome.status != wantStatus {
			t.Fatalf("async catalog status=%d want=%d body=%s", outcome.status, wantStatus, outcome.body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("async catalog request did not finish after its lock was released")
	}
}

// TestCatalogBindRejectsConcurrentModelIdentityChange proves the
// identity assertion re-verified under the model row lock: an operator page
// that loaded the old identity cannot label metadata for a renamed model.
func TestCatalogBindRejectsConcurrentModelIdentityChange(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog CAS Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	revision := matchPreview["catalog_revision"].(string)

	// Phase A: the operator page reads the old identity...
	staleIdentity := requestJSONStatus[map[string]any](t, harness, http.MethodGet, fmt.Sprintf("/api/models/%d", modelConfigID), nil, nil, http.StatusOK)
	if staleIdentity["model_id"] != "gpt-contract" {
		t.Fatalf("fixture model identity drifted: %+v", staleIdentity["model_id"])
	}
	// Phase B: ...a concurrent edit renames the model id before the bind...
	modelJSON[map[string]any](t, harness, profileID, http.MethodPut, fmt.Sprintf("/api/models/%d", modelConfigID), map[string]any{
		"api_family": "openai", "model_id": "gpt-contract-renamed", "is_enabled": false,
	}, http.StatusOK)
	// Phase A again: ...and the manual bind carrying the stale identity must
	// reject with the stable conflict and write nothing. The manual path is
	// used deliberately: auto-matching re-derives its candidates from the
	// current identity and would fail earlier with a different error.
	rejected := modelResponse(t, harness, profileID, http.MethodPost, catalogPath+"/bind", catalogBindBody("gpt-contract", map[string]any{
		"provider_id": "azure", "catalog_model_id": "shared-model", "expected_catalog_revision": revision,
	}))
	catalogAssertErrorContains(t, rejected, http.StatusConflict, "models_dev_model_changed")
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_catalog_bindings WHERE model_config_id = $1`, modelConfigID, 0)

	// The same bind with the fresh identity succeeds, so the guard rejects
	// drift, not the flow itself.
	bound := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/bind", catalogBindBody("gpt-contract-renamed", map[string]any{
		"provider_id": "azure", "catalog_model_id": "shared-model", "expected_catalog_revision": revision,
	}), nil, http.StatusOK)
	if bound["bound"] != true {
		t.Fatalf("fresh-identity bind must succeed: %+v", bound)
	}
}

// TestCatalogRefreshCommitRejectsConcurrentRebind proves the commit cannot
// land against a rebind: the preview token no longer matches the row, so the
// commit rejects and the rebind's coordinate and source survive.
func TestCatalogRefreshCommitRejectsConcurrentRebind(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog CAS Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	revision := matchPreview["catalog_revision"].(string)
	catalogBindAzureModel(t, harness, modelConfigID, revision)

	// A: preview captures azure/shared-model plus the binding token.
	previewA := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/refresh/preview", map[string]any{}, nil, http.StatusOK)
	// B: a concurrent rebind moves the coordinate to openai/gpt-contract.
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/bind", catalogBindBody("gpt-contract", map[string]any{
		"provider_id": "openai", "catalog_model_id": "gpt-contract", "expected_catalog_revision": revision,
	}), nil, http.StatusOK)
	// A: the stale commit must reject with zero writes.
	staleCommit := modelResponse(t, harness, profileID, http.MethodPost, catalogPath+"/refresh/commit", catalogRefreshCommitBody(previewA, revision))
	catalogAssertErrorContains(t, staleCommit, http.StatusConflict, "models_dev_binding_stale")

	binding := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	if binding["provider_id"] != "openai" || binding["catalog_model_id"] != "gpt-contract" {
		t.Fatalf("rebind coordinate must survive the stale commit: %+v", binding)
	}
	if source := binding["source"].(map[string]any); source["name"] != "GPT Contract" {
		t.Fatalf("rebind source must survive the stale commit: %+v", source)
	}
}

// TestCatalogRefreshCommitRejectsConcurrentOverride proves an override written
// between refresh preview and commit both rejects the commit and survives it.
func TestCatalogRefreshCommitRejectsConcurrentOverride(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog CAS Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	revision := matchPreview["catalog_revision"].(string)
	bound := catalogBindAzureModel(t, harness, modelConfigID, revision)

	previewA := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/refresh/preview", map[string]any{}, nil, http.StatusOK)
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, catalogPath+"/override",
		catalogOverrideBody(bound, map[string]any{"name": "Override Between Phases"}), nil, http.StatusOK)
	staleCommit := modelResponse(t, harness, profileID, http.MethodPost, catalogPath+"/refresh/commit", catalogRefreshCommitBody(previewA, revision))
	catalogAssertErrorContains(t, staleCommit, http.StatusConflict, "models_dev_binding_stale")

	binding := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	override := binding["override"].(map[string]any)
	if override == nil || override["name"] != "Override Between Phases" {
		t.Fatalf("concurrent override must survive the stale refresh commit: %+v", override)
	}
	// The refresh never landed: the source name is still the catalog value.
	if source := binding["source"].(map[string]any); source["name"] == "Override Between Phases" {
		t.Fatalf("stale refresh must not write source values: %+v", binding)
	}
}

// TestCatalogUnbindRejectsConcurrentRebind proves an operator page holding a
// stale snapshot cannot delete a newer binding.
func TestCatalogUnbindRejectsConcurrentRebind(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog CAS Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	revision := matchPreview["catalog_revision"].(string)
	catalogBindAzureModel(t, harness, modelConfigID, revision)

	// A reads the binding it is about to delete...
	snapshot := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	// ...B rebinds to another offering...
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/bind", catalogBindBody("gpt-contract", map[string]any{
		"provider_id": "openai", "catalog_model_id": "gpt-contract", "expected_catalog_revision": revision,
	}), nil, http.StatusOK)
	// ...and A's delete of the old coordinate rejects while the new binding
	// stays intact.
	staleUnbind := modelResponse(t, harness, profileID, http.MethodDelete, catalogPath, catalogUnbindBody(snapshot))
	catalogAssertErrorContains(t, staleUnbind, http.StatusConflict, "models_dev_binding_stale")
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_catalog_bindings WHERE model_config_id = $1`, modelConfigID, 1)

	// An unbind carrying the fresh snapshot still works.
	fresh := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	requestJSONStatus[map[string]any](t, harness, http.MethodDelete, catalogPath, catalogUnbindBody(fresh), nil, http.StatusOK)
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_catalog_bindings WHERE model_config_id = $1`, modelConfigID, 0)
}

// TestCatalogOverrideRejectsConcurrentRebind proves a sparse draft opened for
// one offering cannot be applied to whichever offering happens to be bound
// when the request reaches the server.
func TestCatalogOverrideRejectsConcurrentRebind(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog CAS Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	revision := matchPreview["catalog_revision"].(string)
	staleBinding := catalogBindAzureModel(t, harness, modelConfigID, revision)

	requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/bind", catalogBindBody("gpt-contract", map[string]any{
		"provider_id": "openai", "catalog_model_id": "gpt-contract", "expected_catalog_revision": revision,
	}), nil, http.StatusOK)
	staleOverride := modelResponse(t, harness, profileID, http.MethodPut, catalogPath+"/override",
		catalogOverrideBody(staleBinding, map[string]any{"name": "Wrong Offering"}))
	catalogAssertErrorContains(t, staleOverride, http.StatusConflict, "models_dev_binding_stale")

	binding := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	if binding["provider_id"] != "openai" || binding["catalog_model_id"] != "gpt-contract" {
		t.Fatalf("new binding must survive stale override: %+v", binding)
	}
	if binding["override"] != nil {
		t.Fatalf("stale override must write nothing to the new offering: %+v", binding["override"])
	}
}

// TestCatalogOverrideClearRejectsStaleToken proves the destructive all-field
// restore cannot erase an override written after the confirmation snapshot.
func TestCatalogOverrideClearRejectsStaleToken(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog CAS Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	revision := matchPreview["catalog_revision"].(string)
	bound := catalogBindAzureModel(t, harness, modelConfigID, revision)
	first := requestJSONStatus[map[string]any](t, harness, http.MethodPut, catalogPath+"/override",
		catalogOverrideBody(bound, map[string]any{"name": "First Override"}), nil, http.StatusOK)
	second := requestJSONStatus[map[string]any](t, harness, http.MethodPut, catalogPath+"/override",
		catalogOverrideBody(first, map[string]any{"limit_context": 222222}), nil, http.StatusOK)

	staleClear := modelResponse(t, harness, profileID, http.MethodDelete, catalogPath+"/override", catalogClearOverrideBody(first))
	catalogAssertErrorContains(t, staleClear, http.StatusConflict, "models_dev_binding_stale")
	binding := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	override := binding["override"].(map[string]any)
	if override["name"] != "First Override" || override["limit_context"] != float64(222222) {
		t.Fatalf("stale clear must preserve every newer override: %+v", override)
	}

	cleared := requestJSONStatus[map[string]any](t, harness, http.MethodDelete, catalogPath+"/override",
		catalogClearOverrideBody(second), nil, http.StatusOK)
	if cleared["override"] != nil {
		t.Fatalf("fresh clear must restore every field to source: %+v", cleared)
	}
}

// TestCatalogSparseOverridesDoNotLoseConcurrentFields is the lost-update
// proof. The first writer's transaction is held open with the row lock (its
// read phase), the second override runs through the HTTP route (it must block
// on the lock), then the first writer commits its own field. Because the
// route merges over the locked current row — not a pre-lock snapshot — both
// fields survive. A full-row rewrite from a stale record would erase the
// first writer's field.
func TestCatalogSparseOverridesDoNotLoseConcurrentFields(t *testing.T) {
	lockBarrier := newCatalogModelLockBarrier()
	defer lockBarrier.releaseWriter()
	harness := newCatalogContractHarness(t, lockBarrier.observe)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog CAS Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	revision := matchPreview["catalog_revision"].(string)
	bound := catalogBindAzureModel(t, harness, modelConfigID, revision)

	// Writer A takes the row lock inside an explicit transaction. The lock
	// acquisition is the synchronization point; no sleep anywhere.
	tx, err := harness.conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin writer A transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var updatedAt time.Time
	if err := tx.QueryRow(context.Background(),
		`SELECT updated_at FROM model_catalog_bindings WHERE model_config_id = $1 FOR UPDATE`,
		modelConfigID).Scan(&updatedAt); err != nil {
		t.Fatalf("writer A row lock: %v", err)
	}

	// Writer B goes through the HTTP route. The channel barrier pauses it after
	// the model lock and before any binding read; it controls when A commits but
	// is not itself a product assertion. The final HTTP/GET facts below are.
	lockBarrier.arm(t)
	overrideDone := startCatalogJSONRequest(t, harness, http.MethodPut, catalogPath+"/override",
		catalogOverrideBody(bound, map[string]any{"limit_context": 777777}))
	lockBarrier.wait(t, modelConfigID)

	// A commits its own sparse override while holding the lock.
	if _, err := tx.Exec(context.Background(),
		`UPDATE model_catalog_bindings SET override_name = 'Writer A', updated_at = now() WHERE model_config_id = $1`,
		modelConfigID); err != nil {
		t.Fatalf("writer A override write: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("writer A commit: %v", err)
	}
	lockBarrier.releaseWriter()
	awaitCatalogJSONResult(t, overrideDone, http.StatusOK)

	binding := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	override := binding["override"].(map[string]any)
	if override["name"] != "Writer A" {
		t.Fatalf("lock-first writer's field must survive: %+v", override)
	}
	if limitContext, ok := override["limit_context"].(float64); !ok || limitContext != 777777 {
		t.Fatalf("route override must merge over the locked row, not a stale snapshot: %+v", override)
	}
}

// TestCatalogSameFieldConcurrencySerializesLastWriter proves a same-field
// race produces the row-lock-ordered last writer instead of a resurrected
// older value, and that the loser's other override columns are untouched.
func TestCatalogSameFieldConcurrencySerializesLastWriter(t *testing.T) {
	lockBarrier := newCatalogModelLockBarrier()
	defer lockBarrier.releaseWriter()
	harness := newCatalogContractHarness(t, lockBarrier.observe)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog CAS Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	revision := matchPreview["catalog_revision"].(string)
	bound := catalogBindAzureModel(t, harness, modelConfigID, revision)
	// Seed the unrelated field before the blocker transaction so the controlled
	// interleaving concerns only the two name writes below.
	if _, err := harness.conn.Exec(context.Background(),
		`UPDATE model_catalog_bindings SET override_limit_context = 111111 WHERE model_config_id = $1`,
		modelConfigID); err != nil {
		t.Fatalf("seed unrelated override field: %v", err)
	}

	// The first writer holds the binding row while the HTTP writer pauses after
	// its model lock; the pre-seeded unrelated column must survive both writes.
	tx, err := harness.conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin first writer transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var updatedAt time.Time
	if err := tx.QueryRow(context.Background(),
		`SELECT updated_at FROM model_catalog_bindings WHERE model_config_id = $1 FOR UPDATE`,
		modelConfigID).Scan(&updatedAt); err != nil {
		t.Fatalf("first writer row lock: %v", err)
	}
	lockBarrier.arm(t)
	lastWriterDone := startCatalogJSONRequest(t, harness, http.MethodPut, catalogPath+"/override",
		catalogOverrideBody(bound, map[string]any{"name": "Last Writer"}))
	lockBarrier.wait(t, modelConfigID)

	// The first writer commits its own name override, then releases the lock;
	// the HTTP writer's name wins the lock order and must not restore any
	// other column from its pre-lock read.
	if _, err := tx.Exec(context.Background(),
		`UPDATE model_catalog_bindings SET override_name = 'First Writer' WHERE model_config_id = $1`,
		modelConfigID); err != nil {
		t.Fatalf("first writer name write: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("first writer commit: %v", err)
	}
	lockBarrier.releaseWriter()
	awaitCatalogJSONResult(t, lastWriterDone, http.StatusOK)

	binding := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	override := binding["override"].(map[string]any)
	if override["name"] != "Last Writer" {
		t.Fatalf("row-lock order must decide the last writer: %+v", override)
	}
	if limitContext, ok := override["limit_context"].(float64); !ok || limitContext != 111111 {
		t.Fatalf("override-only write must not resurrect other columns: %+v", override)
	}
}

// TestCatalogBindingUpdatedTokenAdvancesUnderFixedClock proves the CAS token
// strictly advances on every real write even when the clock never moves: two
// successive override writes under a frozen Now must produce strictly
// increasing updated_at values, so a preview/commit cycle can never replay a
// consumed token.
func TestCatalogBindingUpdatedTokenAdvancesUnderFixedClock(t *testing.T) {
	// The models service runs on a frozen clock: both override writes below
	// propose the same instant, so only the monotonic token helper can move
	// updated_at. The catalog client still serves the fixture revision.
	frozen := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	harness := newCatalogContractHarnessWithClock(t, frozen)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog Fixed Clock Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	revision := matchPreview["catalog_revision"].(string)
	bound := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/bind", catalogBindBody("gpt-contract", map[string]any{
		"expected_catalog_revision": revision,
	}), nil, http.StatusOK)

	first := requestJSONStatus[map[string]any](t, harness, http.MethodPut, catalogPath+"/override",
		catalogOverrideBody(bound, map[string]any{"name": "Clock Write One"}), nil, http.StatusOK)
	second := requestJSONStatus[map[string]any](t, harness, http.MethodPut, catalogPath+"/override",
		catalogOverrideBody(first, map[string]any{"name": "Clock Write Two"}), nil, http.StatusOK)

	firstAt := parseBindingTimestamp(t, first["updated_at"])
	secondAt := parseBindingTimestamp(t, second["updated_at"])
	if !secondAt.After(firstAt) {
		t.Fatalf("updated_at must strictly advance under a fixed clock: first=%s second=%s", firstAt, secondAt)
	}
}

// newCatalogContractHarnessWithClock builds the catalog harness with the
// models service pinned to one clock instant, so real writes cannot lean on
// wall-clock progress to change the binding CAS token.
func newCatalogContractHarnessWithClock(t *testing.T, now time.Time) *contractHarness {
	t.Helper()
	catalogServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"catalog-contract-1"`)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, catalogFixtureCatalog)
	}))
	t.Cleanup(catalogServer.Close)
	catalogClient, clientErr := modelsdev.NewClient(modelsdev.ClientOptions{BaseURL: catalogServer.URL, HTTPClient: catalogServer.Client()})
	if clientErr != nil {
		t.Fatalf("build catalog client: %v", clientErr)
	}
	return newContractHarnessFor(t, "catalog_fixed_clock", contractHarnessOptions{
		SecretEncryptionKey: "catalog-contract-secret",
		Version:             "catalog-contract-test",
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
			endpointsService, err := managementendpoints.NewService(settings, managementendpoints.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build endpoints service: %v", err)
			}
			t.Cleanup(endpointsService.Close)
			connectionsService, connectionsErr := managementconnections.NewService(settings, managementconnections.Options{Pool: pool, Catalog: catalogClient})
			if connectionsErr != nil {
				t.Fatalf("build connections service: %v", connectionsErr)
			}
			t.Cleanup(connectionsService.Close)
			modelsService, modelsErr := managementmodels.NewService(settings, managementmodels.Options{
				Pool: pool, Catalog: catalogClient, Now: func() time.Time { return now },
			})
			if modelsErr != nil {
				t.Fatalf("build models service: %v", modelsErr)
			}
			t.Cleanup(modelsService.Close)
			modelsService.SetTerminalTargetCreator(connectionsService)
			return platformhttp.Dependencies{
				EndpointsService:   endpointsService,
				ConnectionsService: connectionsService,
				ModelsService:      modelsService,
			}
		},
	})
}

func parseBindingTimestamp(t *testing.T, value any) time.Time {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("expected timestamp string, got %+v", value)
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		t.Fatalf("parse binding timestamp %q: %v", text, err)
	}
	return parsed
}
