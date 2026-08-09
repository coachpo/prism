// Package modelrouting owns Prism's internal model routing graph semantics.
//
// It centralizes access-target type/default/ordering rules, same-family
// compatibility checks, target resolution, and cycle detection while management
// adapters keep their existing connection-shaped wire terms.
//
// It also owns the static routing diagnostics analyzer (coverage.go,
// diagnostics.go) and the structured configuration-warning contract
// (warnings.go): pure two-stage operation coverage over an authored graph
// snapshot, shared by management diagnostics routes, mutation warning
// envelopes and runtime planning rejection classification. The analyzer never
// reads or changes Ban/retry state, admission counters, round-robin cursors or
// endpoint secrets, and never sends provider requests.
package modelrouting
