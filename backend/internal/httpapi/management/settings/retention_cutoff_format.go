package settings

// Retention cutoff formatting owns the wire normalization for custom cleanup
// instants. Inputs must be UTC, must not be future values, and are truncated
// to microseconds before they enter a request hash or sealed job scope.
//
// The formatter emits one fixed RFC3339 layout with six fractional digits.
// This keeps semantically equal timestamps identical in preflight replay,
// manual-job binding, and operator-facing responses.
//
// Policy-day cutoffs remain owned by retention_policy_classifier.go. This file
// handles only explicit timestamp text and its canonical representation.
//
// No database, HTTP writer, or settings service state belongs in this module.
// Keeping the conversion pure makes the frozen cutoff easy to audit.
//
// RFC3339Nano is accepted on input so clients may send a precise timestamp.
// The output deliberately narrows that precision to microseconds because the
// sealed retention contract stores and hashes that exact representation.
//
// A timestamp with a non-UTC offset is rejected even if it denotes the same
// instant as UTC. The wire contract uses the trailing Z as an explicit proof
// that the operator supplied the retention clock expected by the worker.
//
// Future values are rejected against the server reference time, not a browser
// clock. This keeps preflight impact, confirmation, and job execution aligned.
//
// Formatting nil returns nil so optional manual cleanup selection remains an
// absent field rather than an invented zero instant.
//
// These rules are shared by preflight normalization and manual-job binding.
// Keeping them in one file prevents a route from accepting a timestamp that
// the worker later serializes differently.
//
// Error text remains intentionally short because the settings problem adapter
// supplies the public recovery code around it.
//
// A normalized cutoff is reused by impact estimation, preflight hashing, and
// manual-job creation. Any owner that needs another time representation must
// convert from this canonical value rather than parse the original request a
// second time.
//
// This prevents two equal-looking requests from acquiring different durable
// operation identities because of fractional-second formatting.
// The formatter is therefore part of the frozen request identity contract.
//
//
// The owner worker consumes the same canonical text emitted here.
//
// This module deliberately has no fallback timezone.
// A malformed cutoff remains a validation failure.
// No default date is synthesized.
//
import (
	"fmt"
	"strings"
	"time"
)

func parseRetentionCutoff(value string, now time.Time) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !strings.HasSuffix(trimmed, "Z") {
		return time.Time{}, fmt.Errorf("retention cutoff must be UTC")
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil || !parsed.UTC().Equal(parsed) || parsed.After(now.UTC()) {
		return time.Time{}, fmt.Errorf("invalid retention cutoff")
	}
	// The wire contract canonicalizes accepted custom cutoffs to UTC
	// microseconds before hashing and before the sealed job scope is created.
	return parsed.UTC().Truncate(time.Microsecond), nil
}

func formatRetentionCutoff(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format("2006-01-02T15:04:05.000000Z")
	return &formatted
}
