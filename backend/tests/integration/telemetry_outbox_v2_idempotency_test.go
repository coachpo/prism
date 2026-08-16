package integrationtest

import (
	"context"
	"testing"
	"time"
)

// TestTelemetryOutboxV2IdentityIdempotency verifies the v2 outbox identity
// contract (Observe SPEC §3.5): the unique metadata identity
// {profile_id, ingress_request_id} (schema_version=2 partial index) and the
// artifact identity {profile_id, ingress_request_id, component_key,
// artifact_kind} are enforced by the DB, so a duplicate enqueue converges
// instead of creating a second metadata row, and the artifact upsert path is
// idempotent on the same stable key.
func TestTelemetryOutboxV2IdentityIdempotency(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	_, pool := upgradeStateDatabase(t, testContext, harness, "outbox_v2_identity")

	profileID := loadUpgradeDefaultProfileID(t, testContext, pool)
	now := time.Now().UTC()
	ingress := "ingress-identity-idem"

	insertOutboxMetadata := func() {
		t.Helper()
		if _, err := pool.Exec(testContext, `INSERT INTO runtime_telemetry_outbox
			(profile_id, ingress_request_id, schema_version, lifecycle_state, payload, core_payload, created_at)
			VALUES ($1, $2, 2, 'finalized', '{}'::jsonb, $3, $4)`,
			profileID, ingress, `{"kind":"metadata"}`, now); err != nil {
			t.Fatalf("insert v2 metadata row: %v", err)
		}
	}

	// First insert lands; a second bare insert with the same identity must be
	// rejected by the partial unique index (no second metadata row).
	insertOutboxMetadata()
	if _, err := pool.Exec(testContext, `INSERT INTO runtime_telemetry_outbox
		(profile_id, ingress_request_id, schema_version, lifecycle_state, payload, core_payload, created_at)
		VALUES ($1, $2, 2, 'finalized', '{}'::jsonb, $3, $4)`,
		profileID, ingress, `{"kind":"metadata-dup"}`, now); err == nil {
		t.Fatal("expected duplicate v2 metadata identity to be rejected by the unique index")
	}

	// The enqueue retry path converges with ON CONFLICT DO UPDATE: still one row.
	if _, err := pool.Exec(testContext, `INSERT INTO runtime_telemetry_outbox
		(profile_id, ingress_request_id, schema_version, lifecycle_state, payload, core_payload, created_at)
		VALUES ($1, $2, 2, 'finalized', '{}'::jsonb, $3, $4)
		ON CONFLICT (profile_id, ingress_request_id) WHERE schema_version = 2 DO UPDATE SET
			core_payload = EXCLUDED.core_payload,
			lifecycle_state = EXCLUDED.lifecycle_state`,
		profileID, ingress, `{"kind":"metadata-retry"}`, now); err != nil {
		t.Fatalf("converging metadata upsert: %v", err)
	}
	var metadataCount int
	if err := pool.QueryRow(testContext, `SELECT COUNT(*) FROM runtime_telemetry_outbox
		WHERE profile_id = $1 AND ingress_request_id = $2 AND schema_version = 2`, profileID, ingress).Scan(&metadataCount); err != nil {
		t.Fatalf("count metadata rows: %v", err)
	}
	if metadataCount != 1 {
		t.Fatalf("expected exactly 1 v2 metadata row after converging retry, got %d", metadataCount)
	}

	// Artifact identity: same stable key converges via ON CONFLICT DO NOTHING.
	insertArtifact := func(componentKey, payload string) {
		t.Helper()
		if _, err := pool.Exec(testContext, `INSERT INTO runtime_telemetry_artifacts (
			profile_id, ingress_request_id, component_key, artifact_kind, opaque_item_id,
			schema_version, lifecycle_state, payload, observed_bytes, stored_bytes, truncated,
			audit_component_created_at, created_at, updated_at
		) VALUES ($1, $2, $3, 'request_body', $4, 2, 'finalized', $5, 4, 4, FALSE, $6, $6, $6)
		ON CONFLICT (profile_id, ingress_request_id, component_key, artifact_kind) DO NOTHING`,
			profileID, ingress, componentKey, "opaque-"+componentKey, payload, now); err != nil {
			t.Fatalf("insert artifact %s: %v", componentKey, err)
		}
	}
	insertArtifact("launch:1", `{"body":"first"}`)
	insertArtifact("launch:1", `{"body":"retry-duplicate"}`)
	var artifactCount int
	if err := pool.QueryRow(testContext, `SELECT COUNT(*) FROM runtime_telemetry_artifacts
		WHERE profile_id = $1 AND ingress_request_id = $2`, profileID, ingress).Scan(&artifactCount); err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	if artifactCount != 1 {
		t.Fatalf("expected exactly 1 artifact row after idempotent retry, got %d", artifactCount)
	}
}
