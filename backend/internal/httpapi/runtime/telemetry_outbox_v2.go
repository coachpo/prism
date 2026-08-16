package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Telemetry outbox v2 split (Requests SPEC §5.1): the single metadata item per
// ingress never carries raw audit bodies or header blocks. Each request body,
// the final response body, and each launched attempt's irreversibly scrubbed
// header pair is a separate durable artifact item with a stable identity:
//
//	metadata : {profile_id, ingress_request_id, item_kind=metadata}
//	artifact: {profile_id, ingress_request_id, component_key, artifact_kind}
//
// component_key is `launch:<immutable launch ordinal>` for upstream attempts
// and a stable row-kind key for diagnostic components. The DB UNIQUE
// constraints plus opaque item IDs make enqueue retries idempotent; no bare
// INSERT can create a second ingress metadata row.

// v2OutboxItemKind values for runtime_telemetry_outbox.item_kind.
const (
	outboxItemKindMetadata = "metadata"
)

// v2ArtifactKind values for runtime_telemetry_artifacts.artifact_kind.
const (
	artifactKindRequestBody  = "request_body"
	artifactKindResponseBody = "response_body"
	artifactKindHeaders      = "headers"
)

// v2ArtifactComponentKeyPrefix is the stable upstream launch key prefix.
const v2ArtifactComponentKeyPrefix = "launch:"

// v2MetadataPayload is the serialized core of the unique metadata item. It
// contains the full request/usage/accounting facts and the audit presentation
// metadata, but never raw bodies or pre-scrub header values.
type v2MetadataPayload struct {
	Envelope  runtimeTelemetryEnvelope `json:"envelope"`
	Artifacts []v2ArtifactDescriptor   `json:"artifacts,omitempty"`
}

// v2ArtifactDescriptor describes one durable artifact item (the bytes live in
// the artifact table, never in the metadata payload).
type v2ArtifactDescriptor struct {
	ComponentKey            string    `json:"component_key"`
	ArtifactKind            string    `json:"artifact_kind"`
	RequestLogAttemptNumber int       `json:"request_log_attempt_number"`
	ProfileID               int       `json:"profile_id"`
	IngressRequestID        string    `json:"ingress_request_id"`
	CreatedAt               time.Time `json:"created_at"`
}

// v2ArtifactPayload is the serialized body of one artifact item.
type v2ArtifactPayload struct {
	// RequestBody / ResponseBody are the exact stored raw byte prefixes
	// (base64). Header blocks are the canonical scrubbed entries.
	RequestBody                       *string `json:"request_body,omitempty"`
	ResponseBody                      *string `json:"response_body,omitempty"`
	RequestHeaders                    *string `json:"request_headers,omitempty"`
	ResponseHeaders                   *string `json:"response_headers,omitempty"`
	ObservedBytes                     int64   `json:"observed_bytes"`
	StoredBytes                       int64   `json:"stored_bytes"`
	Truncated                         bool    `json:"truncated"`
	Encoding                          *string `json:"encoding,omitempty"`
	CaptureStatus                     *string `json:"capture_status,omitempty"`
	CaptureLimitReason                *string `json:"capture_limit_reason,omitempty"`
	CaptureEndState                   *string `json:"capture_end_state,omitempty"`
	RequestBodyTruncated              bool    `json:"request_body_truncated,omitempty"`
	ResponseBodyTruncated             bool    `json:"response_body_truncated,omitempty"`
	RequestHeadersScrubProvenance     string  `json:"request_headers_scrub_provenance,omitempty"`
	ResponseHeadersScrubProvenance    string  `json:"response_headers_scrub_provenance,omitempty"`
	RequestHeadersCaptureStatus       string  `json:"request_headers_capture_status,omitempty"`
	ResponseHeadersCaptureStatus      string  `json:"response_headers_capture_status,omitempty"`
	RequestHeadersCaptureLimitReason  string  `json:"request_headers_capture_limit_reason,omitempty"`
	ResponseHeadersCaptureLimitReason string  `json:"response_headers_capture_limit_reason,omitempty"`
	RequestHeadersTruncated           bool    `json:"request_headers_truncated,omitempty"`
	ResponseHeadersTruncated          bool    `json:"response_headers_truncated,omitempty"`
	RequestHeadersEntriesObserved     *int    `json:"request_headers_entries_observed,omitempty"`
	RequestHeadersEntriesStored       *int    `json:"request_headers_entries_stored,omitempty"`
	ResponseHeadersEntriesObserved    *int    `json:"response_headers_entries_observed,omitempty"`
	ResponseHeadersEntriesStored      *int    `json:"response_headers_entries_stored,omitempty"`
	RequestHeadersBytesObserved       *int64  `json:"request_headers_bytes_observed,omitempty"`
	RequestHeadersBytesStored         *int64  `json:"request_headers_bytes_stored,omitempty"`
	ResponseHeadersBytesObserved      *int64  `json:"response_headers_bytes_observed,omitempty"`
	ResponseHeadersBytesStored        *int64  `json:"response_headers_bytes_stored,omitempty"`
}

// splitEnvelopeIntoV2Items splits a runtime telemetry envelope into one
// metadata payload (no raw bodies/headers) plus artifact descriptors whose
// bytes are written separately. The audit metadata rows keep their non-raw
// fields in the envelope; the raw body/header columns are cleared from the
// envelope copy before it enters the metadata payload.
func splitEnvelopeIntoV2Items(envelope runtimeTelemetryEnvelope) (v2MetadataPayload, []v2AuditArtifactSplit) {
	metadata := v2MetadataPayload{Envelope: envelope}
	splits := make([]v2AuditArtifactSplit, 0, len(envelope.AuditLogs)*2)
	// Clear raw body/header content from the metadata envelope copy.
	for index := range metadata.Envelope.AuditLogs {
		auditLog := metadata.Envelope.AuditLogs[index]
		attemptNumber := auditLog.RequestLogAttemptNumber
		componentKey := fmt.Sprintf("%s%d", v2ArtifactComponentKeyPrefix, attemptNumber)
		descriptor := v2ArtifactDescriptor{
			ComponentKey:            componentKey,
			ProfileID:               auditLog.ProfileID,
			IngressRequestID:        envelope.UsageEvent.IngressRequestID,
			RequestLogAttemptNumber: attemptNumber,
			CreatedAt:               auditLog.CreatedAt,
		}
		// Headers artifact (scrubbed representation only).
		headersPayload := v2ArtifactPayload{
			RequestHeaders:                    auditHeadersJSONPtr(auditLog.RequestHeaders),
			ResponseHeaders:                   auditLog.ResponseHeaders,
			RequestHeadersScrubProvenance:     auditLog.RequestHeadersScrubProvenance,
			ResponseHeadersScrubProvenance:    auditLog.ResponseHeadersScrubProvenance,
			RequestHeadersCaptureStatus:       auditLog.RequestHeadersCaptureStatus,
			ResponseHeadersCaptureStatus:      auditLog.ResponseHeadersCaptureStatus,
			RequestHeadersCaptureLimitReason:  auditLog.RequestHeadersCaptureLimitReason,
			ResponseHeadersCaptureLimitReason: auditLog.ResponseHeadersCaptureLimitReason,
		}
		headersDescriptor := descriptor
		headersDescriptor.ArtifactKind = artifactKindHeaders
		splits = append(splits, v2AuditArtifactSplit{Descriptor: headersDescriptor, Payload: headersPayload})
		metadata.Artifacts = append(metadata.Artifacts, headersDescriptor)

		// Request body artifact (exact stored raw prefix).
		if auditLog.RequestBody != nil {
			requestDescriptor := descriptor
			requestDescriptor.ArtifactKind = artifactKindRequestBody
			requestPayload := v2ArtifactPayload{
				RequestBody:        auditLog.RequestBody,
				ObservedBytes:      derefInt64(auditLog.RequestBodyBytesObserved),
				StoredBytes:        derefInt64(auditLog.RequestBodyBytesStored),
				Truncated:          auditLog.RequestBodyTruncated,
				Encoding:           auditLog.RequestBodyEncoding,
				CaptureStatus:      optionalStringPtr(auditLog.RequestBodyCaptureStatus),
				CaptureLimitReason: optionalStringPtr(auditLog.RequestBodyCaptureLimitReason),
				CaptureEndState:    auditLog.RequestBodyCaptureEndState,
			}
			splits = append(splits, v2AuditArtifactSplit{Descriptor: requestDescriptor, Payload: requestPayload})
			metadata.Artifacts = append(metadata.Artifacts, requestDescriptor)
		}

		// Final response body artifact (only the final attempt may carry one).
		if auditLog.ResponseBody != nil {
			responseDescriptor := descriptor
			responseDescriptor.ArtifactKind = artifactKindResponseBody
			responsePayload := v2ArtifactPayload{
				ResponseBody:       auditLog.ResponseBody,
				ObservedBytes:      derefInt64(auditLog.ResponseBodyBytesObserved),
				StoredBytes:        derefInt64(auditLog.ResponseBodyBytesStored),
				Truncated:          auditLog.ResponseBodyTruncated,
				Encoding:           auditLog.ResponseBodyEncoding,
				CaptureStatus:      optionalStringPtr(auditLog.ResponseBodyCaptureStatus),
				CaptureLimitReason: optionalStringPtr(auditLog.ResponseBodyCaptureLimitReason),
				CaptureEndState:    auditLog.ResponseBodyCaptureEndState,
			}
			splits = append(splits, v2AuditArtifactSplit{Descriptor: responseDescriptor, Payload: responsePayload})
			metadata.Artifacts = append(metadata.Artifacts, responseDescriptor)
		}

		// Clear raw bytes from the envelope copy that enters the metadata
		// payload: the metadata item must never carry raw bodies or headers.
		metadata.Envelope.AuditLogs[index].RequestHeaders = ""
		metadata.Envelope.AuditLogs[index].ResponseHeaders = nil
		metadata.Envelope.AuditLogs[index].RequestBody = nil
		metadata.Envelope.AuditLogs[index].ResponseBody = nil
	}
	return metadata, splits
}

type v2AuditArtifactSplit struct {
	Descriptor v2ArtifactDescriptor
	Payload    v2ArtifactPayload
}

func auditHeadersJSONPtr(serialized string) *string {
	if serialized == "" || serialized == "{}" {
		return nil
	}
	return &serialized
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func optionalStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// enqueueV2 writes the unique metadata item plus all artifact items in one
// transaction. The metadata row uses schema_version=2 with the stable
// identity; artifact rows use their stable component keys. Retries are
// idempotent via the DB UNIQUE constraints.
func (o *runtimeTelemetryOutbox) enqueueV2(ctx context.Context, envelope runtimeTelemetryEnvelope, lifecycleState string) (int64, error) {
	if o == nil || o.telemetryPool == nil {
		return 0, fmt.Errorf("runtime telemetry outbox unavailable")
	}
	if err := o.enqueueError(); err != nil {
		return 0, err
	}
	o.mu.Lock()
	closed := o.closed
	o.mu.Unlock()
	if closed {
		return 0, fmt.Errorf("runtime telemetry outbox closed")
	}

	metadata, splits := splitEnvelopeIntoV2Items(envelope)
	if envelope.UsageEvent.IngressRequestID == "" {
		slog.Error("v2 enqueue with empty ingress", "state", lifecycleState, "request_logs", len(envelope.RequestLogs), "usage_created", envelope.UsageEvent.CreatedAt.Format(time.RFC3339))
	}
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		return 0, fmt.Errorf("marshal v2 metadata payload: %w", err)
	}

	var rowID int64
	err = pgxutilInTx(ctx, o.telemetryPool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO runtime_telemetry_outbox (profile_id, ingress_request_id, schema_version, lifecycle_state, payload, core_payload, created_at)
			VALUES ($1, $2, 2, $3, '{}'::jsonb, $4, $5)
			ON CONFLICT (profile_id, ingress_request_id) WHERE schema_version = 2 DO UPDATE SET
				core_payload = EXCLUDED.core_payload,
				lifecycle_state = EXCLUDED.lifecycle_state
			RETURNING id`,
			envelope.UsageEvent.ProfileID,
			envelope.UsageEvent.IngressRequestID,
			lifecycleState,
			rawMetadata,
			envelope.UsageEvent.CreatedAt.UTC(),
		).Scan(&rowID); err != nil {
			return fmt.Errorf("enqueue v2 metadata item (ingress=%q state=%s): %w", envelope.UsageEvent.IngressRequestID, lifecycleState, err)
		}
		for _, split := range splits {
			rawPayload, marshalErr := json.Marshal(split.Payload)
			if marshalErr != nil {
				return fmt.Errorf("marshal v2 artifact payload: %w", marshalErr)
			}
			opaqueItemID := newRuntimeUUIDv4()
			if _, err := tx.Exec(ctx, `INSERT INTO runtime_telemetry_artifacts (
				profile_id, ingress_request_id, component_key, artifact_kind, opaque_item_id,
				schema_version, lifecycle_state, payload, observed_bytes, stored_bytes, truncated,
				audit_component_created_at, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, 2, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (profile_id, ingress_request_id, component_key, artifact_kind) DO NOTHING`,
				split.Descriptor.ProfileID,
				split.Descriptor.IngressRequestID,
				split.Descriptor.ComponentKey,
				split.Descriptor.ArtifactKind,
				opaqueItemID,
				lifecycleState,
				rawPayload,
				split.Payload.ObservedBytes,
				split.Payload.StoredBytes,
				split.Payload.Truncated,
				split.Descriptor.CreatedAt.UTC(),
				envelope.UsageEvent.CreatedAt.UTC(),
				envelope.UsageEvent.CreatedAt.UTC(),
			); err != nil {
				return fmt.Errorf("enqueue v2 artifact %s:%s: %w", split.Descriptor.ComponentKey, split.Descriptor.ArtifactKind, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	o.signal()
	return rowID, nil
}

// finalizeV2StreamingAccepted CAS-promotes a provisional_stream metadata item
// to finalized and upserts the terminal artifacts (Requests SPEC §5.1
// two-phase state machine). Ambiguous commit/retry hits the same keys.
func (o *runtimeTelemetryOutbox) finalizeV2StreamingAccepted(ctx context.Context, rowID int64, envelope runtimeTelemetryEnvelope) error {
	ctx = runtimeDetachedContext(ctx)
	if rowID <= 0 {
		return fmt.Errorf("runtime streaming telemetry accepted row id required")
	}
	if o == nil || o.telemetryPool == nil {
		return fmt.Errorf("runtime telemetry outbox unavailable")
	}
	if err := o.enqueueError(); err != nil {
		return err
	}
	metadata, splits := splitEnvelopeIntoV2Items(envelope)
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal v2 finalized metadata payload: %w", err)
	}
	err = pgxutilInTx(ctx, o.telemetryPool, func(tx pgx.Tx) error {
		commandTag, err := tx.Exec(ctx, `UPDATE runtime_telemetry_outbox SET lifecycle_state = 'finalized', core_payload = $1, created_at = $2
			WHERE id = $3 AND schema_version = 2 AND lifecycle_state = 'provisional_stream'`,
			rawMetadata, envelope.UsageEvent.CreatedAt.UTC(), rowID)
		if err != nil {
			return fmt.Errorf("finalize v2 streaming metadata: %w", err)
		}
		if commandTag.RowsAffected() != 1 {
			return fmt.Errorf("runtime streaming telemetry accepted row %d unavailable", rowID)
		}
		for _, split := range splits {
			rawPayload, marshalErr := json.Marshal(split.Payload)
			if marshalErr != nil {
				return fmt.Errorf("marshal v2 finalized artifact payload: %w", marshalErr)
			}
			opaqueItemID := newRuntimeUUIDv4()
			// UPSERT: the accepted phase may not have known the final response
			// body or headers yet; the same stable key is reused so ambiguous
			// commit/retry stays idempotent.
			if _, err := tx.Exec(ctx, `INSERT INTO runtime_telemetry_artifacts (
				profile_id, ingress_request_id, component_key, artifact_kind, opaque_item_id,
				schema_version, lifecycle_state, payload, observed_bytes, stored_bytes, truncated,
				audit_component_created_at, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, 2, 'finalized', $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (profile_id, ingress_request_id, component_key, artifact_kind) DO UPDATE SET
				lifecycle_state = 'finalized',
				payload = EXCLUDED.payload,
				observed_bytes = EXCLUDED.observed_bytes,
				stored_bytes = EXCLUDED.stored_bytes,
				truncated = EXCLUDED.truncated,
				updated_at = now()`,
				split.Descriptor.ProfileID,
				split.Descriptor.IngressRequestID,
				split.Descriptor.ComponentKey,
				split.Descriptor.ArtifactKind,
				opaqueItemID,
				rawPayload,
				split.Payload.ObservedBytes,
				split.Payload.StoredBytes,
				split.Payload.Truncated,
				split.Descriptor.CreatedAt.UTC(),
				envelope.UsageEvent.CreatedAt.UTC(),
				envelope.UsageEvent.CreatedAt.UTC(),
			); err != nil {
				return fmt.Errorf("finalize v2 artifact %s:%s: %w", split.Descriptor.ComponentKey, split.Descriptor.ArtifactKind, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	o.signal()
	return nil
}

// v2MetadataRow is the materializer's view of one metadata item.
type v2MetadataRow struct {
	ID          int64
	ProfileID   int
	IngressID   string
	CorePayload []byte
	CreatedAt   time.Time
}

// loadNextV2MetadataRow selects the oldest finalized metadata item whose core
// is pending, skipping orphaned/provisional items.
func loadNextV2MetadataRow(ctx context.Context, tx pgx.Tx) (v2MetadataRow, bool, error) {
	var row v2MetadataRow
	err := tx.QueryRow(ctx, `SELECT id, profile_id, ingress_request_id, core_payload, created_at
		FROM runtime_telemetry_outbox
		WHERE schema_version = 2 AND lifecycle_state = 'finalized' AND core_state = 'pending'
		ORDER BY id ASC LIMIT 1 FOR UPDATE SKIP LOCKED`).
		Scan(&row.ID, &row.ProfileID, &row.IngressID, &row.CorePayload, &row.CreatedAt)
	if err == pgx.ErrNoRows {
		return v2MetadataRow{}, false, nil
	}
	if err != nil {
		return v2MetadataRow{}, false, fmt.Errorf("load v2 metadata row: %w", err)
	}
	return row, true, nil
}

// loadArtifactsForIngress loads all finalized artifacts for one ingress.
func loadArtifactsForIngress(ctx context.Context, tx pgx.Tx, profileID int, ingressID string) ([]v2ArtifactSplit, error) {
	rows, err := tx.Query(ctx, `SELECT component_key, artifact_kind, payload FROM runtime_telemetry_artifacts
		WHERE profile_id = $1 AND ingress_request_id = $2 AND lifecycle_state = 'finalized' ORDER BY id ASC`, profileID, ingressID)
	if err != nil {
		return nil, fmt.Errorf("load v2 artifacts for ingress %s: %w", ingressID, err)
	}
	defer rows.Close()
	items := make([]v2ArtifactSplit, 0)
	for rows.Next() {
		var item v2ArtifactSplit
		var payload []byte
		if err := rows.Scan(&item.Descriptor.ComponentKey, &item.Descriptor.ArtifactKind, &payload); err != nil {
			return nil, fmt.Errorf("scan v2 artifact row: %w", err)
		}
		if err := json.Unmarshal(payload, &item.Payload); err != nil {
			return nil, fmt.Errorf("decode v2 artifact payload: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate v2 artifact rows: %w", err)
	}
	return items, nil
}

type v2ArtifactSplit struct {
	Descriptor v2ArtifactDescriptor
	Payload    v2ArtifactPayload
}

// mergeArtifactsIntoEnvelope reattaches scrubbed header/body artifacts to the
// audit rows of the envelope after materialization began (stable-key merge).
func mergeArtifactsIntoEnvelope(envelope *runtimeTelemetryEnvelope, artifacts []v2ArtifactSplit) {
	for index := range envelope.AuditLogs {
		attemptNumber := envelope.AuditLogs[index].RequestLogAttemptNumber
		componentKey := fmt.Sprintf("%s%d", v2ArtifactComponentKeyPrefix, attemptNumber)
		for _, artifact := range artifacts {
			if artifact.Descriptor.ComponentKey != componentKey {
				continue
			}
			auditLog := &envelope.AuditLogs[index]
			switch artifact.Descriptor.ArtifactKind {
			case artifactKindHeaders:
				auditLog.RequestHeaders = derefString(artifact.Payload.RequestHeaders)
				auditLog.ResponseHeaders = artifact.Payload.ResponseHeaders
				auditLog.RequestHeadersScrubProvenance = artifact.Payload.RequestHeadersScrubProvenance
				auditLog.ResponseHeadersScrubProvenance = artifact.Payload.ResponseHeadersScrubProvenance
				auditLog.RequestHeadersCaptureStatus = artifact.Payload.RequestHeadersCaptureStatus
				auditLog.ResponseHeadersCaptureStatus = artifact.Payload.ResponseHeadersCaptureStatus
				auditLog.RequestHeadersCaptureLimitReason = artifact.Payload.RequestHeadersCaptureLimitReason
				auditLog.ResponseHeadersCaptureLimitReason = artifact.Payload.ResponseHeadersCaptureLimitReason
			case artifactKindRequestBody:
				auditLog.RequestBody = artifact.Payload.RequestBody
				auditLog.RequestBodyBytesObserved = int64Ptr(artifact.Payload.ObservedBytes)
				auditLog.RequestBodyBytesStored = int64Ptr(artifact.Payload.StoredBytes)
				auditLog.RequestBodyTruncated = artifact.Payload.Truncated
				auditLog.RequestBodyEncoding = artifact.Payload.Encoding
				auditLog.RequestBodyCaptureStatus = derefString(artifact.Payload.CaptureStatus)
				auditLog.RequestBodyCaptureLimitReason = derefString(artifact.Payload.CaptureLimitReason)
				auditLog.RequestBodyCaptureEndState = artifact.Payload.CaptureEndState
			case artifactKindResponseBody:
				auditLog.ResponseBody = artifact.Payload.ResponseBody
				auditLog.ResponseBodyBytesObserved = int64Ptr(artifact.Payload.ObservedBytes)
				auditLog.ResponseBodyBytesStored = int64Ptr(artifact.Payload.StoredBytes)
				auditLog.ResponseBodyTruncated = artifact.Payload.Truncated
				auditLog.ResponseBodyEncoding = artifact.Payload.Encoding
				auditLog.ResponseBodyCaptureStatus = derefString(artifact.Payload.CaptureStatus)
				auditLog.ResponseBodyCaptureLimitReason = derefString(artifact.Payload.CaptureLimitReason)
				auditLog.ResponseBodyCaptureEndState = artifact.Payload.CaptureEndState
			}
		}
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// pgxutilInTx runs fn inside a transaction on the pool.
func pgxutilInTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin telemetry transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit telemetry transaction: %w", err)
	}
	return nil
}
