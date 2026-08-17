package settings

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
