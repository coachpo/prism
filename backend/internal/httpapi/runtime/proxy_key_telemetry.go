package runtime

// Proxy-key telemetry translates the request-context attribution snapshot into
// one durable usage signal. It preserves the runtime auth-enforcement fact and
// delegates the transaction write to proxykeyusage.
//
// A missing or incomplete snapshot is represented by nil rather than a fake
// key. This keeps auth-off, unknown, and identified attribution states distinct
// from one another in the outbox contract.
//
// The helper owns no authentication decision and never performs a live key
// lookup on the runtime request path.
//
// The signal is later inserted in the same telemetry transaction as request
// logs and usage events. A nil signal therefore means omission, not an
// identified key with a zero timestamp.
//
// Auth-enforced is copied from the request-context snapshot rather than
// recomputed from current settings, preserving request-time provenance.
//
// Runtime proxy-key attribution stays permissive when auth is off; this file
// records that fact without changing admission or authorization.
//
// The database writer receives only the bounded key identity and last-use
// fields already captured by request context. Raw credentials never cross this
// seam.
//
// Worker pressure may drop a usage signal according to proxykeyusage policy;
// the proxy response is never delayed for this accounting path.
//
// Usage attribution is historical evidence, not a permission check.
//
//
// The request context is the frozen attribution source.
//
// This file has no access to raw Authorization values.
//
// It also keeps the persistence call in the telemetry transaction owner.
// The signal shape is intentionally small.
// It carries no secret material.
//
import (
	"context"
	"fmt"
	"strings"

	"github.com/coachpo/prism/backend/internal/httpapi/proxykeyusage"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
	"github.com/jackc/pgx/v5"
)

func runtimeProxyKeyUsageSignalFromSnapshot(proxyKey *requestcontext.RuntimeProxyKeySnapshot) *runtimeProxyKeyUsageSignal {
	if proxyKey == nil || proxyKey.ID <= 0 || proxyKey.LastUsedAt.IsZero() {
		return nil
	}
	return &runtimeProxyKeyUsageSignal{
		KeyID:      proxyKey.ID,
		LastUsedAt: proxyKey.LastUsedAt.UTC(),
		LastUsedIP: strings.TrimSpace(proxyKey.LastUsedIP),
	}
}

func runtimeProxyKeyAuthEnforcedFromContext(ctx context.Context) *bool {
	attribution, ok := requestcontext.RuntimeProxyKeyAttributionFromContext(ctx)
	if !ok {
		return nil
	}
	value := attribution.AuthEnforced
	return &value
}

func recordRuntimeProxyKeyUsageTx(ctx context.Context, tx pgx.Tx, signal *runtimeProxyKeyUsageSignal) error {
	if signal == nil {
		return nil
	}
	if err := proxykeyusage.RecordTx(ctx, tx, signal.KeyID, signal.LastUsedAt, signal.LastUsedIP); err != nil {
		return fmt.Errorf("record runtime proxy api key usage: %w", err)
	}
	return nil
}
