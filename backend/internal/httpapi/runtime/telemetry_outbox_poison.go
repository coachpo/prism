package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// Poison-row handling for the runtime telemetry outbox.
//
// Materialization runs inside one transaction. When that transaction aborts,
// every write it made aborts with it — so attempt accounting recorded inside
// it is lost, the row stays exactly as it was, and the strictly FIFO loader
// hands it back on the next tick. A payload the database will never accept
// therefore retries forever and blocks every later row, which stops all
// request recording for the instance while the gateway keeps serving.
//
// The schema already models the way out (core_attempt_count,
// core_next_attempt_at, core_last_safe_error_code, core_state='failed',
// lifecycle_state='core_materialization_failed', runtime_telemetry_quarantine).
// This file implements it.

const (
	// A constraint or data-shape violation cannot become insertable by waiting,
	// so it is retired on the first failure. Everything else gets a bounded
	// number of backed-off retries before it is treated as poison too.
	transientMaterializationAttemptLimit = 8
	poisonBackoffBase                    = 250 * time.Millisecond
	poisonBackoffCap                     = 5 * time.Minute
)

// materializationVerdict classifies why a row could not be materialized.
type materializationVerdict struct {
	Permanent bool
	// SafeCode never carries payload text: it is derived from the SQLSTATE and
	// the schema-owned constraint name, both of which are ours, not the
	// caller's.
	SafeCode string
}

// classifyMaterializationFailure decides whether waiting could ever help.
func classifyMaterializationFailure(err error) materializationVerdict {
	if err == nil {
		return materializationVerdict{}
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		code := pgErr.Code
		safe := "pg_" + code
		if pgErr.ConstraintName != "" {
			safe += ":" + pgErr.ConstraintName
		}
		switch {
		// Class 23 integrity constraint violation, class 22 data exception:
		// the payload itself is unacceptable and always will be.
		case len(code) >= 2 && (code[:2] == "23" || code[:2] == "22"):
			return materializationVerdict{Permanent: true, SafeCode: safe}
		default:
			return materializationVerdict{Permanent: false, SafeCode: safe}
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return materializationVerdict{Permanent: false, SafeCode: "context_cancelled"}
	}
	return materializationVerdict{Permanent: false, SafeCode: "materialization_error"}
}

func poisonBackoffFor(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 16 {
		attempt = 16
	}
	delay := time.Duration(float64(poisonBackoffBase) * math.Pow(2, float64(attempt)))
	if delay > poisonBackoffCap || delay <= 0 {
		return poisonBackoffCap
	}
	return delay
}

// recordMaterializationFailure accounts for one failed attempt on its own
// connection, so the bookkeeping survives the rollback of the attempt that
// produced it. A row judged poison is moved to quarantine and removed from the
// queue, which is what lets everything behind it drain.
//
// It deliberately takes a context that is not the failed attempt's: during
// shutdown that one may already be cancelled, and losing the accounting is how
// a row becomes immortal.
func (o *runtimeTelemetryOutbox) recordMaterializationFailure(ctx context.Context, row outboxMetadataRow, cause error) error {
	verdict := classifyMaterializationFailure(cause)

	var attemptCount int
	quarantined := false
	err := pgxutil.InTx(ctx, o.telemetryPool, "runtime_telemetry_poison", func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			UPDATE runtime_telemetry_outbox
			   SET core_attempt_count = core_attempt_count + 1,
			       core_last_safe_error_code = $2
			 WHERE id = $1
			 RETURNING core_attempt_count`, row.ID, verdict.SafeCode).Scan(&attemptCount); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Someone else retired the row already; nothing to account for.
				return nil
			}
			return fmt.Errorf("account telemetry materialization failure for row %d: %w", row.ID, err)
		}

		retire := verdict.Permanent || attemptCount >= transientMaterializationAttemptLimit
		if !retire {
			if _, err := tx.Exec(ctx, `
				UPDATE runtime_telemetry_outbox
				   SET core_next_attempt_at = now() + make_interval(secs => $2)
				 WHERE id = $1`, row.ID, poisonBackoffFor(attemptCount).Seconds()); err != nil {
				return fmt.Errorf("schedule telemetry retry for row %d: %w", row.ID, err)
			}
			return nil
		}

		// The quarantine row is the durable record, so the queue entry and its
		// artifacts are removed exactly as the success path removes them.
		// Leaving a terminal row behind would turn the outbox into a graveyard
		// and strand its captured bodies, and would break the codebase's own
		// reading of an empty outbox as "no pending work".
		if _, err := tx.Exec(ctx, `
			INSERT INTO runtime_telemetry_quarantine (
				profile_id, ingress_request_id, schema_version, extension_payload, schema_error_code, created_at
			) VALUES ($1, $2, 2, $3, $4, now())`,
			row.ProfileID, row.IngressID, row.CorePayload, verdict.SafeCode); err != nil {
			return fmt.Errorf("quarantine telemetry row %d: %w", row.ID, err)
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM runtime_telemetry_artifacts WHERE profile_id = $1 AND ingress_request_id = $2`,
			row.ProfileID, row.IngressID); err != nil {
			return fmt.Errorf("discard artifacts for quarantined telemetry row %d: %w", row.ID, err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM runtime_telemetry_outbox WHERE id = $1`, row.ID); err != nil {
			return fmt.Errorf("retire quarantined telemetry row %d: %w", row.ID, err)
		}
		quarantined = true
		return nil
	})
	if err != nil {
		return err
	}
	if quarantined {
		// Losing a request's telemetry is never routine. Without this line the
		// only symptom an operator sees is a gap in the Requests page.
		slog.Error("telemetry row quarantined; its request will not appear in request logs",
			"row_id", row.ID, "profile_id", row.ProfileID, "ingress_request_id", row.IngressID,
			"attempts", attemptCount, "safe_error_code", verdict.SafeCode, "permanent", verdict.Permanent)
	}
	return nil
}
