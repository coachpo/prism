package managementjobs

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Job-center cancel and pagination errors are compared by identity so the HTTP
// layer maps them to typed problems without parsing messages.
var (
	errLegacyJobNotCancellable          = fmt.Errorf("legacy_job_not_cancellable")
	errJobTerminal                      = fmt.Errorf("job_terminal")
	errPurgeNotCancellable              = fmt.Errorf("purge_not_cancellable")
	errInvalidJobsCursor                = fmt.Errorf("invalid_jobs_cursor")
	errRetentionCancelOperationConflict = fmt.Errorf("retention_cancel_operation_conflict")
)

func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// IsLegacyJobNotCancellable / IsJobTerminal / IsPurgeNotCancellable classify
// cancel errors for the typed problem writer.
func IsLegacyJobNotCancellable(err error) bool { return err == errLegacyJobNotCancellable }
func IsJobTerminal(err error) bool             { return err == errJobTerminal }
func IsPurgeNotCancellable(err error) bool     { return err == errPurgeNotCancellable }
func IsInvalidJobsCursor(err error) bool       { return err == errInvalidJobsCursor }
func IsRetentionCancelOperationConflict(err error) bool {
	return err == errRetentionCancelOperationConflict
}
