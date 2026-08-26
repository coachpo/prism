package modelexport

import "fmt"

// This file owns every typed domain error of the export surface
// (single declaration site; renderers and merges only reference them). HTTP handlers
// map them onto stable wire codes; message text never carries semantics.

// ErrLockedField rejects manual payloads that target locked Prism truth
// (model_id, protocol mapping, base URL, provider slot, credential slots,
// prices).
type ErrLockedField struct{ Field string }

func (e *ErrLockedField) Error() string {
	return fmt.Sprintf("field %q is managed by Prism and cannot be overridden", e.Field)
}

// ErrSensitiveField rejects manual payloads carrying credential-like keys
// anywhere in their recursive structure.
type ErrSensitiveField struct{ Field string }

func (e *ErrSensitiveField) Error() string {
	return fmt.Sprintf("field %q looks like credential material and was rejected", e.Field)
}

// ErrInvalidEnhancement rejects a manual value that cannot be represented by
// the pinned target client's schema. Keeping this typed lets the HTTP boundary
// return a stable 422 instead of misclassifying operator input as a server
// error.
type ErrInvalidEnhancement struct {
	Field  string
	Reason string
}

// ErrTargetSchema reports that the complete generated document would not be
// accepted by the pinned client schema. Renderers run this guard immediately
// before serialization so trusted metadata cannot bypass the same boundary as
// manual values.
type ErrTargetSchema struct {
	Field  string
	Reason string
}

func (e *ErrTargetSchema) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("generated document does not match the target schema: %s", e.Reason)
	}
	return fmt.Sprintf("generated document field %q does not match the target schema: %s", e.Field, e.Reason)
}

func (e *ErrInvalidEnhancement) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("enhancement does not match the target schema: %s", e.Reason)
	}
	return fmt.Sprintf("enhancement field %q does not match the target schema: %s", e.Field, e.Reason)
}

// ErrUnselectableModel rejects render selections containing models that the
// source marked unselectable, do not exist, or belong to another profile.
type ErrUnselectableModel struct {
	ModelConfigID int
	Reason        string
}

func (e *ErrUnselectableModel) Error() string {
	return fmt.Sprintf("model %d is not exportable: %s", e.ModelConfigID, e.Reason)
}

// ErrDefaultModel rejects a platform-incompatible or unselected default.
type ErrDefaultModel struct{ Reason string }

func (e *ErrDefaultModel) Error() string {
	return fmt.Sprintf("default model is invalid: %s", e.Reason)
}

// ErrSourceStale marks a digest drift between source and render. Handlers map
// it onto HTTP 409 with wire code "export_source_stale".
type ErrSourceStale struct{}

func (e *ErrSourceStale) Error() string {
	return "export source facts drifted; refetch /source before rendering"
}
