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
