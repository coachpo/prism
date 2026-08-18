package stats

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Observe query-context contract.
//
// A query context freezes one consistent dataset snapshot: requested bounds,
// effective usage bounds, retention coverage and the signing material for
// fragment cursors. Fragments only accept the opaque signed token; they never
// re-parse presets or wall clocks.

// Coverage describes one dataset's effective coverage over the requested
// window.
type Coverage struct {
	RequestedPreset     string             `json:"requested_preset"`
	FromTime            time.Time          `json:"from_time"`
	ToTime              time.Time          `json:"to_time"`
	RetentionFromTime   *time.Time         `json:"retention_from_time"`
	Source              string             `json:"source"` // raw | rollup | hybrid
	Complete            bool               `json:"complete"`
	Gaps                []CoverageGap      `json:"gaps"`
	Precision           *CoveragePrecision `json:"precision,omitempty"`
	RetentionEpoch      string             `json:"retention_epoch,omitempty"`
	RetentionGeneration string             `json:"retention_generation,omitempty"`
	PurgeState          string             `json:"purge_state,omitempty"`
	SourceRevision      string             `json:"source_revision,omitempty"`
}

type CoverageGap struct {
	FromTime time.Time `json:"from_time"`
	ToTime   time.Time `json:"to_time"`
	Reason   string    `json:"reason"`
}

type CoveragePrecision struct {
	TTFT       string `json:"ttft"`        // exact | approximate
	OutputRate string `json:"output_rate"` // exact | approximate
}

// QueryBounds resolves one requested window into effective dataset bounds.
type QueryBounds struct {
	RequestedPreset    string
	RequestedFrom      *time.Time
	RequestedTo        *time.Time
	UsageFrom          time.Time
	UsageTo            time.Time
	UsageRetentionFrom *time.Time
	Source             string
	Complete           bool
	Gaps               []CoverageGap
}

// ResolveQueryBoundsFromActualCoverage resolves a domain window against the
// owning actual-coverage projection. It is the only query-context path that
// gives `all` a lower bound: a policy duration or product fallback is not
// evidence that the domain contains rows. A dirty projection may still
// provide a diagnostic range, but that range must not be used to shrink the
// SQL interval because the owner has explicitly said it is not current.
func ResolveQueryBoundsFromActualCoverage(preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time, source RetentionFloorEpochSource, actual ActualCoverageProjection) (QueryBounds, error) {
	normalizedPreset := strings.TrimSpace(preset)
	if normalizedPreset == "" {
		normalizedPreset = "24h"
	}
	requestedFrom, requestedTo, err := resolveRequestedWindow(normalizedPreset, fromTime, toTime, referenceNow)
	if err != nil {
		return QueryBounds{}, err
	}
	if normalizedPreset == "all" {
		if actual.Earliest != nil {
			requestedFrom = actual.Earliest.UTC()
			if floor := effectiveCoverageFloor(source); floor != nil && requestedFrom.Before(*floor) {
				requestedFrom = *floor
			}
		} else {
			// An owner-complete empty domain is represented as an empty half-open
			// interval, not as a synthetic 30-day history.
			requestedFrom = requestedTo
		}
	}

	retentionFrom := effectiveCoverageFloor(source)
	effectiveFrom := requestedFrom
	effectiveTo := requestedTo
	gaps := make([]CoverageGap, 0, len(actual.Gaps)+2)
	complete := actual.Complete && actual.Freshness == "fresh"
	if retentionFrom != nil && effectiveFrom.Before(*retentionFrom) {
		gaps = append(gaps, CoverageGap{FromTime: effectiveFrom, ToTime: *retentionFrom, Reason: "retention_deleted"})
		effectiveFrom = *retentionFrom
		complete = false
	}
	if actual.Freshness == "fresh" && actual.Earliest != nil && effectiveFrom.Before(actual.Earliest.UTC()) {
		gapEnd := actual.Earliest.UTC()
		if gapEnd.Before(effectiveTo) {
			gaps = append(gaps, CoverageGap{FromTime: effectiveFrom, ToTime: gapEnd, Reason: "actual_coverage_unavailable"})
			effectiveFrom = gapEnd
			complete = false
		}
	}
	// latest_retained_at is an observed row timestamp, not an exclusive query
	// boundary. Keep the frozen `to_time` at the shared reference instant so a
	// row exactly at the owner watermark is not silently excluded by the
	// half-open SQL predicate. The owner freshness/completeness state still
	// communicates whether the tail is fully materialized.
	for _, gap := range actual.Gaps {
		from, fromOK := coverageGapTime(gap["from_time"])
		to, toOK := coverageGapTime(gap["to_time"])
		reason, reasonOK := gap["reason"].(string)
		if fromOK && toOK && from.Before(to) && reasonOK && strings.TrimSpace(reason) != "" {
			gaps = append(gaps, CoverageGap{FromTime: from, ToTime: to, Reason: reason})
			complete = false
		}
	}
	if !effectiveFrom.Before(effectiveTo) && normalizedPreset != "all" {
		// Keep a valid requested interval when actual coverage has no overlap;
		// the explicit gap/complete=false state prevents a zero/empty claim.
		effectiveFrom = requestedFrom
		effectiveTo = requestedTo
	}
	return QueryBounds{
		RequestedPreset:    normalizedPreset,
		RequestedFrom:      queryRequestedTimePointer(normalizedPreset, requestedFrom),
		RequestedTo:        queryRequestedTimePointer(normalizedPreset, requestedTo),
		UsageFrom:          effectiveFrom,
		UsageTo:            effectiveTo,
		UsageRetentionFrom: retentionFrom,
		Source:             "raw",
		Complete:           complete && len(gaps) == 0,
		Gaps:               gaps,
	}, nil
}

// normalizeActualCoveragePreset keeps legacy callers that only supplied
// from_time/to_time on the same owner-bound path as the explicit time_range
// contract. A single bound retains the old one-sided-window meaning, while a
// pair of explicit bounds is the legacy spelling of custom.
func normalizeActualCoveragePreset(preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time) (string, *time.Time, *time.Time, error) {
	normalized := strings.TrimSpace(preset)
	if normalized == "" {
		switch {
		case fromTime == nil && toTime == nil:
			normalized = "24h"
		case fromTime == nil && toTime != nil:
			resolvedFrom := toTime.UTC().Add(-24 * time.Hour)
			fromTime = &resolvedFrom
			normalized = "custom"
		case fromTime != nil && toTime == nil:
			resolvedTo := referenceNow.UTC()
			toTime = &resolvedTo
			normalized = "custom"
		default:
			normalized = "custom"
		}
	}
	if normalized == "all" && (fromTime != nil || toTime != nil) {
		return "", nil, nil, &HTTPError{StatusCode: 422, Detail: "time_range=all cannot include explicit bounds"}
	}
	if normalized == "custom" && (fromTime == nil || toTime == nil) {
		return "", nil, nil, &HTTPError{StatusCode: 422, Detail: "time_range=custom requires from_time and to_time"}
	}
	return normalized, fromTime, toTime, nil
}

func queryRequestedTimePointer(preset string, value time.Time) *time.Time {
	if preset == "all" {
		return nil
	}
	return &value
}

func resolveRequestedWindow(preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time) (time.Time, time.Time, error) {
	referenceNow = referenceNow.UTC()
	switch preset {
	case "1h", "6h", "24h", "7d", "30d":
		return referenceNow.Add(-presetDuration(preset)), referenceNow, nil
	case "all":
		return referenceNow, referenceNow, nil
	case "custom":
		if fromTime == nil || toTime == nil {
			return time.Time{}, time.Time{}, &HTTPError{StatusCode: 422, Detail: "preset=custom requires from_time and to_time"}
		}
		start := fromTime.UTC()
		end := toTime.UTC()
		if !end.After(start) {
			return time.Time{}, time.Time{}, &HTTPError{StatusCode: 422, Detail: "invalid_time_range"}
		}
		if end.Sub(start) > 30*24*time.Hour {
			return time.Time{}, time.Time{}, &HTTPError{StatusCode: 422, Detail: "custom range cannot exceed 30 days"}
		}
		return start, end, nil
	default:
		return time.Time{}, time.Time{}, &HTTPError{StatusCode: 422, Detail: fmt.Sprintf("unknown preset %q", preset)}
	}
}

func effectiveCoverageFloor(source RetentionFloorEpochSource) *time.Time {
	floor := source.ConfiguredCutoff
	if source.PublishedFloor != nil && (floor == nil || source.PublishedFloor.After(*floor)) {
		floor = source.PublishedFloor
	}
	if floor == nil {
		return nil
	}
	resolved := floor.UTC()
	return &resolved
}

func coverageGapTime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func presetDuration(preset string) time.Duration {
	switch preset {
	case "1h":
		return time.Hour
	case "6h":
		return 6 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// QueryContextToken is the signed, opaque query context handed to fragments.
// It binds the profile, requested preset, effective usage bounds, coverage
// and a nonce so fragment cursors can be validated without re-parsing.
type QueryContextToken struct {
	SchemaVersion   int                                   `json:"schema_version"`
	ProfileID       int                                   `json:"profile_id"`
	RequestedPreset string                                `json:"requested_preset"`
	RequestedFrom   *string                               `json:"requested_from,omitempty"`
	RequestedTo     *string                               `json:"requested_to,omitempty"`
	UsageFrom       string                                `json:"usage_from"`
	UsageTo         string                                `json:"usage_to"`
	RetentionEpoch  string                                `json:"retention_epoch"`
	SourceRevision  string                                `json:"source_revision"`
	Source          string                                `json:"source"`
	Complete        bool                                  `json:"complete"`
	Domains         map[string]QueryContextDomainSnapshot `json:"domains"`
	IssuedAt        time.Time                             `json:"issued_at"`
	ExpiresAt       time.Time                             `json:"expires_at"`
}

// QueryContextDomainSnapshot freezes the owner evidence needed to interpret
// each Observe domain. MaterializationCut is copied verbatim from the owner;
// the query-context route never synthesizes it from a read timestamp.
type QueryContextDomainSnapshot struct {
	Domain              string         `json:"domain"`
	FromTime            time.Time      `json:"from_time"`
	ToTime              time.Time      `json:"to_time"`
	RetentionFromTime   *time.Time     `json:"retention_from_time,omitempty"`
	RetentionEpoch      string         `json:"retention_epoch"`
	RetentionGeneration string         `json:"retention_generation"`
	FenceGeneration     string         `json:"fence_generation"`
	SourceRevision      string         `json:"source_revision"`
	CoverageRevision    string         `json:"coverage_revision"`
	CoverageHash        string         `json:"coverage_hash"`
	CoverageGeneratedAt *time.Time     `json:"coverage_generated_at,omitempty"`
	MaterializationCut  map[string]any `json:"materialization_cut"`
	Gaps                []CoverageGap  `json:"gaps"`
	Complete            bool           `json:"complete"`
	Freshness           string         `json:"freshness"`
	PurgeState          string         `json:"purge_state"`
}

// CoverageFromQueryBounds projects the coverage frozen into one signed
// query-context domain snapshot. Exact latency precision is authorized only
// by a complete, fresh raw owner snapshot with its signed identity intact;
// incomplete or stale evidence must never be upgraded by a fragment loader.
func CoverageFromQueryBounds(bounds QueryBounds, snapshot QueryContextDomainSnapshot) Coverage {
	gaps := append(make([]CoverageGap, 0, len(bounds.Gaps)), bounds.Gaps...)
	complete := bounds.Complete && snapshot.Complete && snapshot.Freshness == "fresh" && len(gaps) == 0
	var precision *CoveragePrecision
	if complete && bounds.Source == "raw" && queryContextCoverageOwnerReady(snapshot) {
		precision = &CoveragePrecision{TTFT: "exact", OutputRate: "exact"}
	}
	return Coverage{
		RequestedPreset:     bounds.RequestedPreset,
		FromTime:            bounds.UsageFrom.UTC(),
		ToTime:              bounds.UsageTo.UTC(),
		RetentionFromTime:   retentionTime(bounds.UsageRetentionFrom),
		Source:              bounds.Source,
		Complete:            complete,
		Gaps:                gaps,
		Precision:           precision,
		RetentionEpoch:      snapshot.RetentionEpoch,
		RetentionGeneration: snapshot.RetentionGeneration,
		PurgeState:          snapshot.PurgeState,
		SourceRevision:      snapshot.SourceRevision,
	}
}

func queryContextCoverageOwnerReady(snapshot QueryContextDomainSnapshot) bool {
	return strings.TrimSpace(snapshot.Domain) != "" &&
		strings.TrimSpace(snapshot.RetentionEpoch) != "" &&
		strings.TrimSpace(snapshot.RetentionGeneration) != "" &&
		strings.TrimSpace(snapshot.FenceGeneration) != "" &&
		strings.TrimSpace(snapshot.SourceRevision) != "" &&
		strings.TrimSpace(snapshot.CoverageRevision) != "" &&
		strings.TrimSpace(snapshot.CoverageHash) != "" &&
		snapshot.CoverageGeneratedAt != nil &&
		strings.TrimSpace(snapshot.PurgeState) != ""
}

// QueryBoundsForDomain reconstructs the frozen bounds for one Observe
// consumer. A fragment must use the domain snapshot rather than the usage
// compatibility fields on QueryContextToken; request-log deep links in
// particular have a different actual-coverage owner than usage summaries.
func QueryBoundsForDomain(token QueryContextToken, domain string) (QueryBounds, error) {
	snapshot, ok := token.Domains[domain]
	if !ok || snapshot.Domain != domain {
		return QueryBounds{}, fmt.Errorf("query context does not contain domain %q", domain)
	}
	return QueryBounds{
		RequestedPreset:    token.RequestedPreset,
		RequestedFrom:      parseQueryContextTime(token.RequestedFrom),
		RequestedTo:        parseQueryContextTime(token.RequestedTo),
		UsageFrom:          snapshot.FromTime.UTC(),
		UsageTo:            snapshot.ToTime.UTC(),
		UsageRetentionFrom: snapshot.RetentionFromTime,
		Source:             "raw",
		Complete:           snapshot.Complete,
		Gaps:               append([]CoverageGap(nil), snapshot.Gaps...),
	}, nil
}

func parseQueryContextTime(value *string) *time.Time {
	if value == nil {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

const queryContextSchemaVersion = 1
const queryContextTTL = 24 * time.Hour

// SignQueryContext creates the base64url opaque token (signature appended).
func SignQueryContext(token QueryContextToken, signingKey []byte) (string, error) {
	payload, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := hmacSHA256(signingKey, []byte("prism.observe.query-context.v1\x00"+encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// VerifyQueryContext parses and verifies an opaque token, returning the token
// or a typed HTTP error (410 when expired).
func VerifyQueryContext(raw string, signingKey []byte, referenceNow time.Time) (QueryContextToken, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return QueryContextToken{}, &HTTPError{StatusCode: 422, Detail: "invalid query_context"}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return QueryContextToken{}, &HTTPError{StatusCode: 422, Detail: "invalid query_context"}
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return QueryContextToken{}, &HTTPError{StatusCode: 422, Detail: "invalid query_context"}
	}
	expected := hmacSHA256(signingKey, []byte("prism.observe.query-context.v1\x00"+parts[0]))
	if !hmac.Equal(expected, signature) {
		return QueryContextToken{}, &HTTPError{StatusCode: 422, Detail: "invalid query_context"}
	}
	var token QueryContextToken
	if err := json.Unmarshal(payload, &token); err != nil {
		return QueryContextToken{}, &HTTPError{StatusCode: 422, Detail: "invalid query_context"}
	}
	if token.SchemaVersion != queryContextSchemaVersion {
		return QueryContextToken{}, &HTTPError{StatusCode: 422, Detail: "invalid query_context"}
	}
	if referenceNow.UTC().After(token.ExpiresAt) {
		return QueryContextToken{}, &HTTPError{StatusCode: 410, Detail: "query_context_expired"}
	}
	return token, nil
}

// DeriveQuerySigningKey derives a domain-separated HMAC subkey from the server
// secret encryption key (never the raw key bytes).
func DeriveQuerySigningKey(secretEncryptionKey string) []byte {
	mac := hmac.New(sha256.New, []byte(secretEncryptionKey))
	_, _ = mac.Write([]byte("prism.observe.query-context.v1"))
	return mac.Sum(nil)
}

func hmacSHA256(key []byte, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil)
}

// QueryContextResponse is the query-context route payload.
type QueryContextResponse struct {
	QueryContext    string      `json:"query_context"`
	RequestedBounds *TimeBounds `json:"requested_bounds"`
	UsageBounds     TimeBounds  `json:"usage_bounds"`
	UsageCoverage   Coverage    `json:"usage_coverage"`
	EventBounds     TimeBounds  `json:"event_bounds"`
	EventCoverage   Coverage    `json:"event_coverage"`
	RequestBounds   TimeBounds  `json:"request_bounds"`
	RequestCoverage Coverage    `json:"request_coverage"`
	GeneratedAt     time.Time   `json:"generated_at"`
}

type TimeBounds struct {
	FromTime time.Time `json:"from_time"`
	ToTime   time.Time `json:"to_time"`
}
