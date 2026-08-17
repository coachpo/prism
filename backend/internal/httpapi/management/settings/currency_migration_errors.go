package settings

// Currency migration errors form the conflict vocabulary shared by draft
// upload, preview, commit, and archive routes. Each constructor returns the
// package domain error with a stable status and detail code. Callers therefore
// preserve one conflict grammar while keeping route-specific validation close
// to its workflow.
//
// Operation conflicts identify an already-bound operation identity. Draft
// conflicts identify a draft whose content or identity no longer matches.
// State conflicts identify a draft lifecycle state that rejects the requested
// transition. Preview conflicts identify a preview whose CAS inputs changed.
// None of these constructors writes a response or inspects the database.
//
// The exact detail strings are public management contract values. Keep changes
// here synchronized with the settings problem registry and contract tests.
// Error construction stays separate from persistence so retry behavior remains
// explicit at each transaction boundary.
//
// A conflict is not a generic internal failure: its status is deliberately
// `409 Conflict`, and route adapters preserve that distinction for clients.
//
// Draft upload treats a mismatched repeated chunk as a draft conflict. Preview
// treats a changed settings epoch or template revision as a stale preview.
// Commit and archive share the operation conflict because both reserve a
// durable operation identity before their final write.
//
// The constructors intentionally carry no field map. The surrounding settings
// adapter owns any locatable validation details, while these stable codes keep
// replay and stale-state branches searchable in logs and client recovery.
//
// Do not collapse these cases into one generic error: the UI uses the detail
// code to choose re-upload, re-preview, or operation inspection guidance.
//
// Error constructors are intentionally deterministic. They do not include
// database ids, timestamps, prices, or draft contents in Detail, so a conflict
// response remains safe to log and stable across retries.
//
// Route handlers attach request-specific context through the registered
// settings problem writer after receiving one of these values.
//
// The error values are safe to compare through their Detail string because the
// package domain error preserves it exactly. Route adapters may wrap them, but
// must not replace the stable code with SQL driver text.
//
// This ownership also keeps archive conflicts aligned with draft conflicts
// without forcing archive persistence into the draft store.
//
// Keeping this vocabulary in one file also makes the route matrix reviewable:
// every draft lifecycle failure has one named constructor and one status.
//
//
// Detail codes are intentionally shorter than the workflow descriptions.
// Recovery context belongs to the registered problem envelope.
//
// No constructor performs logging or persistence.
// The route remains the owner of request context.
// The persistence layer remains the owner of durable state.
// These boundaries keep the conflict vocabulary side-effect free.
//
import (
	"fmt"
	"net/http"
)

func currencyMigrationOperationConflict() error {
	return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_operation_conflict: operation identity is already bound to a different draft or payload"}
}

func currencyMigrationDraftConflict() error {
	return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_conflict: draft identity or content does not match"}
}

func currencyMigrationDraftStateError(state string) error {
	return &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_state_%s", state)}
}

func currencyMigrationPreviewStale() error {
	return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_stale: preview no longer matches the sealed draft or current settings"}
}
