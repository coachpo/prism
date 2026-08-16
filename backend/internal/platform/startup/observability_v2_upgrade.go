package startup

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// runObservabilityV2Upgrade executes the exclusive offline v1 drain and the
// three-domain legacy scrub backfill after migrations applied (Requests SPEC
// §5.6). It is crash-resumable: every batch commits its checkpoint in the
// same transaction, and a restart continues from the durable cursor. The
// upgrade state machine is:
//
//	draining_v1 -> (v1 outbox count = 0) -> v1_drained
//	v1_drained -> (all domains ready) -> backfill_ready -> final (000011)
//
// Fresh installs are already backfill_ready and skip straight through.
func (s Service) runObservabilityV2Upgrade(ctx context.Context, conn *pgx.Conn) error {
	drainOwner := newRuntimeTelemetryV1DrainOwner(s.timestamp)
	backfillOwner := newRequestAuditV2BackfillOwner(s.timestamp)

	// Drain pass: loop until the v1 outbox is empty or the state no longer
	// requires draining. Each batch commits its own transaction.
	for {
		var remaining int
		var complete bool
		err := pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
			var err error
			remaining, complete, err = drainOwner.Run(ctx, tx)
			return err
		})
		if err != nil {
			return fmt.Errorf("run v1 telemetry drain: %w", err)
		}
		if complete || remaining == 0 {
			break
		}
	}

	// Backfill pass: loop until all profiles/domains are ready.
	for {
		var complete bool
		err := pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
			var err error
			complete, err = backfillOwner.EnsureAllDomainsReady(ctx, tx)
			return err
		})
		if err != nil {
			return fmt.Errorf("run observability v2 backfill: %w", err)
		}
		if complete {
			return nil
		}
	}
}
