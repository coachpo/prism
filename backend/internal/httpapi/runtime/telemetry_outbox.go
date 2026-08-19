package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/background"
)

const (
	defaultRuntimeTelemetryOutboxWorkerCount     = 1
	defaultRuntimeTelemetryOutboxPollInterval    = 250 * time.Millisecond
	defaultRuntimeTelemetryOutboxShutdownTimeout = 3 * time.Second
	defaultRuntimeTelemetryOutboxWakeupBuffer    = 1
)

type TelemetryOutboxOptions struct {
	WorkerCount     int
	PollInterval    time.Duration
	ShutdownTimeout time.Duration
	WakeupBuffer    int
	Hooks           *TelemetryOutboxHooks
	Scheduler       *background.Scheduler
}

type TelemetryOutboxCloseResult struct {
	Drained     bool
	TimedOut    bool
	PendingRows int
	Inflight    int
	Elapsed     time.Duration
}

type TelemetryOutboxHooks struct {
	EnqueueError      func() error
	BeforeMaterialize func(context.Context) error
	AfterClose        func(TelemetryOutboxCloseResult)
}

type runtimeTelemetryOutbox struct {
	telemetryPool   *pgxpool.Pool
	now             func() time.Time
	logPartitions   *runtimeLogPartitionCache
	pollInterval    time.Duration
	shutdownTimeout time.Duration
	hooks           TelemetryOutboxHooks
	wake            chan struct{}
	scheduler       *background.Scheduler
	ownsScheduler   bool
	closeOnce       sync.Once
	mu              sync.Mutex
	closed          bool
	inflight        int
	closeResult     TelemetryOutboxCloseResult
}

type runtimeTelemetryMaterializationResult struct {
	Processed    bool
	RequestLogID int
	ProfileID    int
}

type runtimeTelemetryDrainState struct {
	PendingRows int
	Inflight    int
}

func (state runtimeTelemetryDrainState) drained() bool {
	return state.PendingRows == 0 && state.Inflight == 0
}

func newRuntimeTelemetryOutbox(telemetryPool *pgxpool.Pool, now func() time.Time, logPartitions *runtimeLogPartitionCache, options TelemetryOutboxOptions) *runtimeTelemetryOutbox {
	normalized := normalizeTelemetryOutboxOptions(options)
	outbox := &runtimeTelemetryOutbox{
		telemetryPool:   telemetryPool,
		now:             now,
		logPartitions:   logPartitions,
		pollInterval:    normalized.PollInterval,
		shutdownTimeout: normalized.ShutdownTimeout,
		hooks:           normalized.hooks(),
		wake:            make(chan struct{}, normalized.WakeupBuffer),
		scheduler:       normalized.Scheduler,
	}
	if outbox.scheduler == nil {
		outbox.scheduler = background.NewScheduler(background.Config{})
		outbox.ownsScheduler = true
		_ = outbox.RegisterBackgroundWorker(outbox.scheduler)
		_ = outbox.scheduler.Start(context.Background())
	}
	outbox.signal()
	return outbox
}

func (o *runtimeTelemetryOutbox) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if o == nil || scheduler == nil {
		return nil
	}
	o.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name:             background.WorkerName("runtime_telemetry_outbox"),
		Priority:         background.PriorityLowBackground,
		MaxPriority:      background.PriorityLowBackground,
		QueueLimit:       128,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainBestEffort,
		CoalescePolicy:   background.CoalesceDropNew,
		RetryPolicy:      &background.RetryPolicy{MaxAttempts: 3, Delay: o.pollInterval},
		PeriodicTrigger:  &background.PeriodicTrigger{Interval: o.pollInterval},
		Timeout:          o.shutdownTimeout,
	}, o.handleScheduledTelemetry)
}

func (o *runtimeTelemetryOutbox) Enqueue(ctx context.Context, envelope runtimeTelemetryEnvelope) error {
	_, err := o.enqueue(ctx, envelope)
	return err
}

func (o *runtimeTelemetryOutbox) EnqueueStreamingAccepted(ctx context.Context, envelope runtimeTelemetryEnvelope) (int64, error) {
	envelope.HandoffPhase = runtimeTelemetryHandoffPhaseStreamAccepted
	return o.enqueueOutboxItems(ctx, envelope, "provisional_stream")
}

func (o *runtimeTelemetryOutbox) FinalizeStreamingAccepted(ctx context.Context, rowID int64, envelope runtimeTelemetryEnvelope) error {
	return o.finalizeStreamingAcceptedItems(ctx, rowID, envelope)
}

func (o *runtimeTelemetryOutbox) enqueue(ctx context.Context, envelope runtimeTelemetryEnvelope) (int64, error) {
	if envelope.TraceContext.empty() {
		envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	return o.enqueueOutboxItems(ctx, envelope, "finalized")
}

func (o *runtimeTelemetryOutbox) Close() TelemetryOutboxCloseResult {
	if o == nil {
		return TelemetryOutboxCloseResult{Drained: true}
	}
	o.closeOnce.Do(func() {
		startedAt := time.Now()
		o.mu.Lock()
		o.closed = true
		o.mu.Unlock()
		deadline := startedAt.Add(o.shutdownTimeout)
		o.signal()
		result := TelemetryOutboxCloseResult{}
		for time.Now().Before(deadline) {
			state, err := o.drainState()
			if err == nil && state.drained() {
				result.Drained = true
				result.PendingRows = state.PendingRows
				result.Inflight = state.Inflight
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if o.scheduler != nil && o.ownsScheduler {
			ctx, cancel := context.WithTimeout(context.Background(), time.Until(deadline))
			_ = o.scheduler.Stop(ctx, deadline)
			cancel()
		}
		if state, err := o.drainState(); err == nil {
			result.Drained = state.drained()
			result.PendingRows = state.PendingRows
			result.Inflight = state.Inflight
		}
		result.TimedOut = !result.Drained
		result.Elapsed = time.Since(startedAt)
		o.mu.Lock()
		o.closeResult = result
		o.mu.Unlock()
		if o.hooks.AfterClose != nil {
			o.hooks.AfterClose(result)
		}
	})
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.closeResult
}

func (o *runtimeTelemetryOutbox) handleScheduledTelemetry(ctx context.Context, _ background.Job) background.JobResult {
	for {
		processed, err := o.processNext(ctx)
		if err != nil {
			slog.Error("runtime telemetry materialization failed; will retry", "error", err)
			return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
		}
		if !processed {
			return background.JobResult{Status: background.JobSucceeded}
		}
	}
}

func (o *runtimeTelemetryOutbox) processNext(ctx context.Context) (bool, error) {
	// Close may have already signalled the scheduler while a periodic job was
	// queued. Do not count an empty post-close wake as in-flight work: otherwise
	// Close can observe the database drained, race with this no-op job starting,
	// and report a false timeout even though no telemetry remains.
	o.mu.Lock()
	closed := o.closed
	o.mu.Unlock()
	if closed {
		state, err := o.drainState()
		if err != nil {
			return false, err
		}
		if state.PendingRows == 0 {
			return false, nil
		}
	}
	o.beginInflight()
	// The claimed row has to escape the transaction as a Go value: when
	// materialization aborts, any accounting written inside that transaction
	// rolls back with it, and a row that is never accounted for is retried
	// forever ahead of everything behind it.
	var claimed *outboxMetadataRow
	result, err := pgxutil.InTxValue(ctx, o.telemetryPool, "runtime_telemetry", func(tx pgx.Tx) (runtimeTelemetryMaterializationResult, error) {
		claimed = nil
		// Prefer the current metadata item; fall back to the legacy v1 envelope.
		metadataRow, metadataFound, metadataErr := loadNextOutboxMetadataRow(ctx, tx)
		if metadataErr != nil {
			return runtimeTelemetryMaterializationResult{}, metadataErr
		}
		if metadataFound {
			claimed = &metadataRow
			return o.materializeOutboxMetadataRow(ctx, tx, metadataRow)
		}
		return runtimeTelemetryMaterializationResult{}, nil
	})
	o.finishInflight()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false, nil
		}
		if claimed != nil {
			// context.WithoutCancel: during shutdown the attempt's context may
			// already be cancelled, and skipping the accounting is exactly how
			// a row becomes immortal.
			if accountErr := o.recordMaterializationFailure(context.WithoutCancel(ctx), *claimed, err); accountErr != nil {
				slog.Error("could not account for a telemetry materialization failure",
					"row_id", claimed.ID, "error", accountErr)
				return false, err
			}
			// The row is now either backed off or quarantined, so the queue can
			// advance. Report progress rather than an error: aborting the drain
			// here is what let one bad row block every later one.
			return true, nil
		}
		return false, err
	}
	if !result.Processed {
		return false, nil
	}
	return true, nil
}

// materializeOutboxMetadataRow materializes a metadata item: decode the core
// payload, reattach the scrubbed header/body artifacts by stable key,
// materialize request/usage/audit/accounting, then ACK the metadata row and
// its artifacts (Requests SPEC §5.1 idempotent merge).
func (o *runtimeTelemetryOutbox) materializeOutboxMetadataRow(ctx context.Context, tx pgx.Tx, row outboxMetadataRow) (runtimeTelemetryMaterializationResult, error) {
	var metadata outboxMetadataPayload
	if err := json.Unmarshal(row.CorePayload, &metadata); err != nil {
		return runtimeTelemetryMaterializationResult{}, fmt.Errorf("decode metadata payload row %d: %w", row.ID, err)
	}
	if metadata.Envelope.TraceContext.empty() {
		metadata.Envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	materializeCtx := metadata.Envelope.TraceContext.context(ctx)
	if o.hooks.BeforeMaterialize != nil {
		if err := o.hooks.BeforeMaterialize(materializeCtx); err != nil {
			return runtimeTelemetryMaterializationResult{}, err
		}
	}
	artifacts, err := loadArtifactsForIngress(materializeCtx, tx, row.ProfileID, row.IngressID)
	if err != nil {
		return runtimeTelemetryMaterializationResult{}, err
	}
	mergeArtifactsIntoEnvelope(&metadata.Envelope, artifacts)
	requestLogID, err := materializeRuntimeTelemetryEnvelopeTx(materializeCtx, tx, o.logPartitions, metadata.Envelope)
	if err != nil {
		return runtimeTelemetryMaterializationResult{}, err
	}
	// ACK the metadata row and its artifacts together; core state advanced
	// first so extension-only retries never re-report usage as pending.
	if _, err := tx.Exec(materializeCtx, `UPDATE runtime_telemetry_outbox SET core_state = 'materialized', core_materialized_at = now(), core_payload = NULL WHERE id = $1`, row.ID); err != nil {
		return runtimeTelemetryMaterializationResult{}, fmt.Errorf("ack metadata row %d: %w", row.ID, err)
	}
	if _, err := tx.Exec(materializeCtx, `DELETE FROM runtime_telemetry_artifacts WHERE profile_id = $1 AND ingress_request_id = $2`, row.ProfileID, row.IngressID); err != nil {
		return runtimeTelemetryMaterializationResult{}, fmt.Errorf("ack artifacts for ingress %s: %w", row.IngressID, err)
	}
	if _, err := tx.Exec(materializeCtx, `DELETE FROM runtime_telemetry_outbox WHERE id = $1 AND core_state = 'materialized'`, row.ID); err != nil {
		return runtimeTelemetryMaterializationResult{}, fmt.Errorf("delete metadata row %d: %w", row.ID, err)
	}
	return runtimeTelemetryMaterializationResult{Processed: true, RequestLogID: requestLogID, ProfileID: row.ProfileID}, nil
}

func (o *runtimeTelemetryOutbox) drainState() (runtimeTelemetryDrainState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	var pendingRows int
	if err := o.telemetryPool.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_telemetry_outbox`).Scan(&pendingRows); err != nil {
		return runtimeTelemetryDrainState{}, err
	}
	o.mu.Lock()
	inflight := o.inflight
	o.mu.Unlock()
	return runtimeTelemetryDrainState{PendingRows: pendingRows, Inflight: inflight}, nil
}

func (o *runtimeTelemetryOutbox) enqueueError() error {
	if o.hooks.EnqueueError == nil {
		return nil
	}
	return o.hooks.EnqueueError()
}

func (o *runtimeTelemetryOutbox) signal() {
	if o.scheduler != nil {
		_ = o.scheduler.Submit(context.Background(), background.JobRequest{Worker: background.WorkerName("runtime_telemetry_outbox"), CoalesceKey: "runtime_telemetry_outbox"})
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *runtimeTelemetryOutbox) beginInflight() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inflight++
}

func (o *runtimeTelemetryOutbox) finishInflight() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.inflight > 0 {
		o.inflight--
	}
}

func normalizeTelemetryOutboxOptions(options TelemetryOutboxOptions) TelemetryOutboxOptions {
	normalized := options
	if normalized.WorkerCount <= 0 {
		normalized.WorkerCount = defaultRuntimeTelemetryOutboxWorkerCount
	}
	if normalized.PollInterval <= 0 {
		normalized.PollInterval = defaultRuntimeTelemetryOutboxPollInterval
	}
	if normalized.ShutdownTimeout <= 0 {
		normalized.ShutdownTimeout = defaultRuntimeTelemetryOutboxShutdownTimeout
	}
	if normalized.WakeupBuffer <= 0 {
		normalized.WakeupBuffer = defaultRuntimeTelemetryOutboxWakeupBuffer
	}
	return normalized
}

func (o TelemetryOutboxOptions) hooks() TelemetryOutboxHooks {
	if o.Hooks == nil {
		return TelemetryOutboxHooks{}
	}
	return *o.Hooks
}

// Outbox item model and persistence share the runtime outbox owner.

// Telemetry outbox item split (Requests SPEC §5.1): the single metadata item per
// ingress never carries raw audit bodies or header blocks. Each request body,
// the final response body, and each launched attempt's irreversibly scrubbed
// header pair is a separate durable artifact item with a stable identity:
//
//	metadata : {profile_id, ingress_request_id, schema_version=2}
//	artifact: {profile_id, ingress_request_id, component_key, artifact_kind}
//
// component_key is `launch:<immutable launch ordinal>` for upstream attempts
// and a stable row-kind key for diagnostic components. The DB UNIQUE
// constraints plus opaque item IDs make enqueue retries idempotent; no bare
// INSERT can create a second ingress metadata row.

// Artifact kinds for runtime_telemetry_artifacts.artifact_kind.
const (
	artifactKindRequestBody  = "request_body"
	artifactKindResponseBody = "response_body"
	artifactKindHeaders      = "headers"
)

// artifactComponentKeyPrefix is the stable upstream launch key prefix.
const artifactComponentKeyPrefix = "launch:"

// outboxMetadataPayload is the serialized core of the unique metadata item. It
// contains the full request/usage/accounting facts and the audit presentation
// metadata, but never raw bodies or pre-scrub header values.
type outboxMetadataPayload struct {
	Envelope  runtimeTelemetryEnvelope   `json:"envelope"`
	Artifacts []outboxArtifactDescriptor `json:"artifacts,omitempty"`
}

// outboxArtifactDescriptor describes one durable artifact item (the bytes live in
// the artifact table, never in the metadata payload).
type outboxArtifactDescriptor struct {
	ComponentKey            string    `json:"component_key"`
	ArtifactKind            string    `json:"artifact_kind"`
	RequestLogAttemptNumber int       `json:"request_log_attempt_number"`
	ProfileID               int       `json:"profile_id"`
	IngressRequestID        string    `json:"ingress_request_id"`
	CreatedAt               time.Time `json:"created_at"`
}

// outboxArtifactPayload is the serialized body of one artifact item.
type outboxArtifactPayload struct {
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

// splitEnvelopeIntoOutboxItems splits a runtime telemetry envelope into one
// metadata payload (no raw bodies/headers) plus artifact descriptors whose
// bytes are written separately. The audit metadata rows keep their non-raw
// fields in the envelope; the raw body/header columns are cleared from the
// envelope copy before it enters the metadata payload.
func splitEnvelopeIntoOutboxItems(envelope runtimeTelemetryEnvelope) (outboxMetadataPayload, []envelopeArtifactSplit) {
	metadata := outboxMetadataPayload{Envelope: envelope}
	splits := make([]envelopeArtifactSplit, 0, len(envelope.AuditLogs)*2)
	// Clear raw body/header content from the metadata envelope copy.
	for index := range metadata.Envelope.AuditLogs {
		auditLog := metadata.Envelope.AuditLogs[index]
		attemptNumber := auditLog.RequestLogAttemptNumber
		componentKey := fmt.Sprintf("%s%d", artifactComponentKeyPrefix, attemptNumber)
		descriptor := outboxArtifactDescriptor{
			ComponentKey:            componentKey,
			ProfileID:               auditLog.ProfileID,
			IngressRequestID:        envelope.UsageEvent.IngressRequestID,
			RequestLogAttemptNumber: attemptNumber,
			CreatedAt:               auditLog.CreatedAt,
		}
		// Headers artifact (scrubbed representation only).
		headersPayload := outboxArtifactPayload{
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
		splits = append(splits, envelopeArtifactSplit{Descriptor: headersDescriptor, Payload: headersPayload})
		metadata.Artifacts = append(metadata.Artifacts, headersDescriptor)

		// Request body artifact (exact stored raw prefix).
		if auditLog.RequestBody != nil {
			requestDescriptor := descriptor
			requestDescriptor.ArtifactKind = artifactKindRequestBody
			requestPayload := outboxArtifactPayload{
				RequestBody:        auditLog.RequestBody,
				ObservedBytes:      derefInt64(auditLog.RequestBodyBytesObserved),
				StoredBytes:        derefInt64(auditLog.RequestBodyBytesStored),
				Truncated:          auditLog.RequestBodyTruncated,
				Encoding:           auditLog.RequestBodyEncoding,
				CaptureStatus:      optionalStringPtr(auditLog.RequestBodyCaptureStatus),
				CaptureLimitReason: optionalStringPtr(auditLog.RequestBodyCaptureLimitReason),
				CaptureEndState:    auditLog.RequestBodyCaptureEndState,
			}
			splits = append(splits, envelopeArtifactSplit{Descriptor: requestDescriptor, Payload: requestPayload})
			metadata.Artifacts = append(metadata.Artifacts, requestDescriptor)
		}

		// Final response body artifact (only the final attempt may carry one).
		if auditLog.ResponseBody != nil {
			responseDescriptor := descriptor
			responseDescriptor.ArtifactKind = artifactKindResponseBody
			responsePayload := outboxArtifactPayload{
				ResponseBody:       auditLog.ResponseBody,
				ObservedBytes:      derefInt64(auditLog.ResponseBodyBytesObserved),
				StoredBytes:        derefInt64(auditLog.ResponseBodyBytesStored),
				Truncated:          auditLog.ResponseBodyTruncated,
				Encoding:           auditLog.ResponseBodyEncoding,
				CaptureStatus:      optionalStringPtr(auditLog.ResponseBodyCaptureStatus),
				CaptureLimitReason: optionalStringPtr(auditLog.ResponseBodyCaptureLimitReason),
				CaptureEndState:    auditLog.ResponseBodyCaptureEndState,
			}
			splits = append(splits, envelopeArtifactSplit{Descriptor: responseDescriptor, Payload: responsePayload})
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

type envelopeArtifactSplit struct {
	Descriptor outboxArtifactDescriptor
	Payload    outboxArtifactPayload
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

// enqueueOutboxItems writes the unique metadata item plus all artifact items in one
// transaction. The metadata row uses schema_version=2 with the stable
// identity; artifact rows use their stable component keys. Retries are
// idempotent via the DB UNIQUE constraints.
func (o *runtimeTelemetryOutbox) enqueueOutboxItems(ctx context.Context, envelope runtimeTelemetryEnvelope, lifecycleState string) (int64, error) {
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

	metadata, splits := splitEnvelopeIntoOutboxItems(envelope)
	if envelope.UsageEvent.IngressRequestID == "" {
		slog.Error("outbox enqueue with empty ingress", "state", lifecycleState, "request_logs", len(envelope.RequestLogs), "usage_created", envelope.UsageEvent.CreatedAt.Format(time.RFC3339))
	}
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		return 0, fmt.Errorf("marshal outbox metadata payload: %w", err)
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
			return fmt.Errorf("enqueue outbox metadata item (ingress=%q state=%s): %w", envelope.UsageEvent.IngressRequestID, lifecycleState, err)
		}
		for _, split := range splits {
			rawPayload, marshalErr := json.Marshal(split.Payload)
			if marshalErr != nil {
				return fmt.Errorf("marshal outbox artifact payload: %w", marshalErr)
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
				return fmt.Errorf("enqueue outbox artifact %s:%s: %w", split.Descriptor.ComponentKey, split.Descriptor.ArtifactKind, err)
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

// finalizeStreamingAcceptedItems CAS-promotes a provisional_stream metadata item
// to finalized and upserts the terminal artifacts (Requests SPEC §5.1
// two-phase state machine). Ambiguous commit/retry hits the same keys.
func (o *runtimeTelemetryOutbox) finalizeStreamingAcceptedItems(ctx context.Context, rowID int64, envelope runtimeTelemetryEnvelope) error {
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
	metadata, splits := splitEnvelopeIntoOutboxItems(envelope)
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal finalized outbox metadata payload: %w", err)
	}
	err = pgxutilInTx(ctx, o.telemetryPool, func(tx pgx.Tx) error {
		commandTag, err := tx.Exec(ctx, `UPDATE runtime_telemetry_outbox SET lifecycle_state = 'finalized', core_payload = $1, created_at = $2
			WHERE id = $3 AND schema_version = 2 AND lifecycle_state = 'provisional_stream'`,
			rawMetadata, envelope.UsageEvent.CreatedAt.UTC(), rowID)
		if err != nil {
			return fmt.Errorf("finalize streaming metadata: %w", err)
		}
		if commandTag.RowsAffected() != 1 {
			return fmt.Errorf("runtime streaming telemetry accepted row %d unavailable", rowID)
		}
		for _, split := range splits {
			rawPayload, marshalErr := json.Marshal(split.Payload)
			if marshalErr != nil {
				return fmt.Errorf("marshal finalized artifact payload: %w", marshalErr)
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
				return fmt.Errorf("finalize outbox artifact %s:%s: %w", split.Descriptor.ComponentKey, split.Descriptor.ArtifactKind, err)
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

// outboxMetadataRow is the materializer's view of one metadata item.
type outboxMetadataRow struct {
	ID          int64
	ProfileID   int
	IngressID   string
	CorePayload []byte
	CreatedAt   time.Time
}

// loadNextOutboxMetadataRow selects the oldest finalized metadata item whose core
// is pending, skipping orphaned/provisional items.
func loadNextOutboxMetadataRow(ctx context.Context, tx pgx.Tx) (outboxMetadataRow, bool, error) {
	var row outboxMetadataRow
	// core_next_attempt_at gates the retry backoff. Without it the loader
	// re-selects a failing row immediately and forever, and because the claim
	// is strictly FIFO that one row blocks every later row behind it.
	err := tx.QueryRow(ctx, `SELECT id, profile_id, ingress_request_id, core_payload, created_at
		FROM runtime_telemetry_outbox
		WHERE schema_version = 2 AND lifecycle_state = 'finalized' AND core_state = 'pending'
			AND (core_next_attempt_at IS NULL OR core_next_attempt_at <= now())
		ORDER BY id ASC LIMIT 1 FOR UPDATE SKIP LOCKED`).
		Scan(&row.ID, &row.ProfileID, &row.IngressID, &row.CorePayload, &row.CreatedAt)
	if err == pgx.ErrNoRows {
		return outboxMetadataRow{}, false, nil
	}
	if err != nil {
		return outboxMetadataRow{}, false, fmt.Errorf("load outbox metadata row: %w", err)
	}
	return row, true, nil
}

// loadArtifactsForIngress loads all finalized artifacts for one ingress.
func loadArtifactsForIngress(ctx context.Context, tx pgx.Tx, profileID int, ingressID string) ([]storedArtifactSplit, error) {
	rows, err := tx.Query(ctx, `SELECT component_key, artifact_kind, payload FROM runtime_telemetry_artifacts
		WHERE profile_id = $1 AND ingress_request_id = $2 AND lifecycle_state = 'finalized' ORDER BY id ASC`, profileID, ingressID)
	if err != nil {
		return nil, fmt.Errorf("load outbox artifacts for ingress %s: %w", ingressID, err)
	}
	defer rows.Close()
	items := make([]storedArtifactSplit, 0)
	for rows.Next() {
		var item storedArtifactSplit
		var payload []byte
		if err := rows.Scan(&item.Descriptor.ComponentKey, &item.Descriptor.ArtifactKind, &payload); err != nil {
			return nil, fmt.Errorf("scan outbox artifact row: %w", err)
		}
		if err := json.Unmarshal(payload, &item.Payload); err != nil {
			return nil, fmt.Errorf("decode outbox artifact payload: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox artifact rows: %w", err)
	}
	return items, nil
}

type storedArtifactSplit struct {
	Descriptor outboxArtifactDescriptor
	Payload    outboxArtifactPayload
}

// mergeArtifactsIntoEnvelope reattaches scrubbed header/body artifacts to the
// audit rows of the envelope after materialization began (stable-key merge).
func mergeArtifactsIntoEnvelope(envelope *runtimeTelemetryEnvelope, artifacts []storedArtifactSplit) {
	for index := range envelope.AuditLogs {
		attemptNumber := envelope.AuditLogs[index].RequestLogAttemptNumber
		componentKey := fmt.Sprintf("%s%d", artifactComponentKeyPrefix, attemptNumber)
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
