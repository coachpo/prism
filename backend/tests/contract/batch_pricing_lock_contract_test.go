package contracttest

import (
	"context"
	"net/http"
	"runtime"
	"testing"
	"time"
)

func TestBatchPricingLocksProfileBeforeTargets(t *testing.T) {
	h := newBatchContractHarness(t)
	modelID, targetID := exportSeedModel(t, h, "batch-lock", "openai", "dual_native")
	_ = modelID
	profile := modelLoadDefaultProfileID(t, h)
	templateID := insertContractPricingTemplate(t, h, profile, "batch-lock-price")
	tx, err := h.conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var id int
	if err := tx.QueryRow(t.Context(), `SELECT id FROM profiles WHERE id=$1 FOR UPDATE`, profile).Scan(&id); err != nil {
		t.Fatal(err)
	}
	pending := startCatalogJSONRequest(t, h, http.MethodPost, "/api/models/batch/target_pricing/preview", map[string]any{"items": []map[string]any{{"id": targetID}}, "reference_id": templateID})
	// Observe the actual database wait instead of racing a sleep against the
	// handler. While it waits for profile serialization it must hold no target.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		err := tx.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM profiles%')`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("batch did not wait on profile before target")
		}
		runtime.Gosched()
	}
	if err := tx.QueryRow(t.Context(), `SELECT id FROM connections WHERE id=$1 FOR UPDATE NOWAIT`, targetID).Scan(&id); err != nil {
		t.Fatalf("batch held target while waiting for profile: %v", err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	awaitCatalogJSONResult(t, pending, http.StatusOK)
	t.Log("BATCH_LOCK_ACCEPTANCE profile_wait_observed=true target_lock_available=true preview_completed=true")
}

func TestBatchPricingRejectsDisabledCapabilityMismatch(t *testing.T) {
	h := newBatchContractHarness(t)
	_, first := exportSeedModel(t, h, "batch-valid-disabled-peer", "openai", "dual_native")
	_, second := exportSeedModel(t, h, "batch-disabled-invalid", "openai", "dual_native")
	template := insertContractPricingTemplate(t, h, 1, "batch-disabled-price")
	if _, err := h.conn.Exec(t.Context(), `UPDATE connections SET is_active=false,openai_text_capability='responses_only' WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	if err := h.conn.QueryRow(t.Context(), `SELECT updated_at FROM connections WHERE id=$1`, first).Scan(&before); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"items": []map[string]any{{"id": first}, {"id": second}}, "reference_id": template, "confirm_manual_overrides": true}
	preview := batchCall(t, h, "target_pricing", "preview", body, http.StatusOK)
	if preview["can_apply"] != false {
		t.Fatal("disabled mismatched target bypassed capability rules")
	}
	body["preview_token"] = preview["preview_token"]
	batchCall(t, h, "target_pricing", "apply", body, http.StatusUnprocessableEntity)
	var after time.Time
	if err := h.conn.QueryRow(t.Context(), `SELECT updated_at FROM connections WHERE id=$1`, first).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !before.Equal(after) {
		t.Fatal("invalid disabled target caused a partial write to its valid peer")
	}
}
