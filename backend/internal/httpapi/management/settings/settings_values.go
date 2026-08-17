package settings

// Settings scalar values provide the nil-preserving projections shared by
// costing, currency migration, inventory, archive, and problem responses.
// `stringValue` turns an optional stored value into the empty-string form used
// in canonical comparisons. `nullableNonEmptyString` preserves the distinction
// between an empty wire value and a meaningful response value.
//
// `intPtr` is the small value projection used when a response needs an owned
// integer pointer. It stays here with the scalar family instead of being
// duplicated in migration cutover and problem construction.
//
// These functions have no database or HTTP dependencies. They do not validate
// currency codes, parse timestamps, or decide migration state; those rules
// remain in their owning modules.
//
// Nil behavior is part of the settings JSON contract. Keep nil and empty
// semantics unchanged when adding another scalar projection.
//
// The empty-string convention is used only where the surrounding contract
// already treats absence as an empty comparison value. It must not be copied
// into persistence writers that need SQL NULL; those writers remain in store.go.
//
// The pointer helper returns a pointer to a local value whose lifetime escapes
// safely under Go allocation rules. Callers receive independent values and do
// not share mutable state with a request DTO.
//
// Keeping these three conversions together gives settings wire projections one
// obvious home. New policy validation belongs in the retention or currency
// module that owns the policy, not in this scalar boundary.
//
// Historical currency responses depend on nil preservation: an absent legacy
// code remains absent, while a configured empty string is intentionally folded
// to nil by nullableNonEmptyString. This distinction keeps archive responses
// honest about whether a source currency existed.
//
// The helper names are package-private because they are implementation seams,
// not an additional settings API.
//
// A scalar projection must stay allocation-safe for concurrent response
// assembly. No helper mutates its input pointer or shares a returned pointer
// with another call.
//
// This boundary is intentionally small and boring: it exists to keep repeated
// nil checks out of workflow modules without hiding policy decisions.
// Callers remain responsible for deciding whether absence is valid, stale, or
// a required field violation.
// The value layer never chooses a recovery code.
//
// These helpers are safe to reuse from archive-only responses because they do
// not imply an active reporting epoch. The caller supplies that semantic
// context explicitly.
//
// Keep this module free of JSON marshaling so wire field ownership remains in
// types.go and the workflow projections.
// The package tests exercise these nil states through their owning contracts.
// No caller should infer pricing readiness from a scalar conversion alone.

// Scalar semantics are intentionally documented here because they are reused
// by both global and instance settings scopes.
// The conversion boundary never changes a stored value.
// Nil remains nil in pointer-returning projections.
// Empty remains empty only in comparison-oriented projections.
//
// This file is not a validation module.
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableNonEmptyString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func intPtr(value int) *int { return &value }
