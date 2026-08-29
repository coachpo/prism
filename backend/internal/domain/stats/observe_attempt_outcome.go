package stats

import (
	"database/sql"
	"strings"
)

type attemptOutcomeClass string

const (
	attemptClassCompleted          attemptOutcomeClass = "completed"
	attemptClassHTTPError          attemptOutcomeClass = "http_error"
	attemptClassStreamError        attemptOutcomeClass = "stream_error"
	attemptClassTransportError     attemptOutcomeClass = "transport_error"
	attemptClassCancelled          attemptOutcomeClass = "cancelled"
	attemptClassClientDisconnected attemptOutcomeClass = "client_disconnected"
	attemptClassUnknown            attemptOutcomeClass = "unknown"
)

func classifyAttemptResult(value sql.NullString) attemptOutcomeClass {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return attemptClassUnknown
	}
	switch attemptOutcomeClass(strings.TrimSpace(value.String)) {
	case attemptClassCompleted:
		return attemptClassCompleted
	case attemptClassHTTPError:
		return attemptClassHTTPError
	case attemptClassStreamError:
		return attemptClassStreamError
	case attemptClassTransportError:
		return attemptClassTransportError
	case attemptClassCancelled:
		return attemptClassCancelled
	case attemptClassClientDisconnected:
		return attemptClassClientDisconnected
	default:
		return attemptClassUnknown
	}
}

func attemptClassIsProblem(class attemptOutcomeClass) bool {
	return class != attemptClassCompleted && class != attemptClassCancelled
}

func abnormalAttemptStreamOutcome(class attemptOutcomeClass, outcome sql.NullString) bool {
	if class == attemptClassStreamError || class == attemptClassClientDisconnected {
		return true
	}
	if !outcome.Valid {
		return false
	}
	value := strings.TrimSpace(outcome.String)
	return value != "" && value != StreamOutcomeNotStreaming && value != StreamOutcomeCompleted
}
