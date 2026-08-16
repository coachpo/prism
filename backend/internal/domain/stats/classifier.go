package stats

import (
	"strings"
)

// Canonical outcome classifier shared by summary, series, errors, breakdowns,
// recent activity, Requests deep links and exports. It must be implemented
// once and reused everywhere; UI must never re-derive final results.

// FinalResult is the mutually exclusive user-visible result classification.
type FinalResult string

const (
	FinalResultCompleted          FinalResult = "completed"
	FinalResultFailed             FinalResult = "failed"
	FinalResultClientDisconnected FinalResult = "client_disconnected"
)

// OutcomeDetail is the mutually exclusive diagnostic detail classification.
type OutcomeDetail string

const (
	OutcomeDetailCompleted          OutcomeDetail = "completed"
	OutcomeDetailHTTPError          OutcomeDetail = "http_error"
	OutcomeDetailStreamError        OutcomeDetail = "stream_error"
	OutcomeDetailClientDisconnected OutcomeDetail = "client_disconnected"
)

// Stream outcome values persisted by the runtime (canonical enum).
const (
	StreamOutcomeNotStreaming = "not_streaming"
	StreamOutcomeCompleted    = "completed"
	// GatewayTimeout is distinct from ClientDisconnected on purpose: the caller
	// is still there, Prism is the one that ended the stream.
	StreamOutcomeGatewayTimeout               = "gateway_timeout"
	StreamOutcomeProviderIncomplete           = "provider_incomplete"
	StreamOutcomeClientDisconnected           = "client_disconnected"
	StreamOutcomeUpstreamReadError            = "upstream_read_error"
	StreamOutcomeUpstreamEndedWithoutTerminal = "upstream_ended_without_terminal"
	StreamOutcomeUnknown                      = "unknown"
)

// ClassifyOutcomeDetail applies the canonical判定顺序:
// http_error (highest) -> client_disconnected -> completed -> stream_error.
func ClassifyOutcomeDetail(statusCode int, streamOutcome *string) OutcomeDetail {
	if statusCode < 200 || statusCode > 299 {
		return OutcomeDetailHTTPError
	}
	outcome := strings.TrimSpace(dereferenceString(streamOutcome))
	if outcome == StreamOutcomeClientDisconnected {
		return OutcomeDetailClientDisconnected
	}
	if outcome == "" || outcome == StreamOutcomeNotStreaming || outcome == StreamOutcomeCompleted {
		return OutcomeDetailCompleted
	}
	return OutcomeDetailStreamError
}

// ClassifyFinalResult maps an outcome detail to the three-value final result.
func ClassifyFinalResult(detail OutcomeDetail) FinalResult {
	switch detail {
	case OutcomeDetailHTTPError, OutcomeDetailStreamError:
		return FinalResultFailed
	case OutcomeDetailClientDisconnected:
		return FinalResultClientDisconnected
	default:
		return FinalResultCompleted
	}
}

// OutcomeCounts holds the mutually exclusive count decomposition for a set of
// requests. Invariants:
//
//	request_count = completed + http_error + stream_error + client_disconnected
//	http_success_count = completed + stream_error + client_disconnected
//	http_failed_count = http_error
//	failed_count = http_error + stream_error
type OutcomeCounts struct {
	RequestCount            int
	CompletedCount          int
	HTTPErrorCount          int
	StreamErrorCount        int
	ClientDisconnectedCount int
}

func (counts OutcomeCounts) HTTPSuccessCount() int {
	return counts.CompletedCount + counts.StreamErrorCount + counts.ClientDisconnectedCount
}

func (counts OutcomeCounts) HTTPFailedCount() int {
	return counts.HTTPErrorCount
}

func (counts OutcomeCounts) FailedCount() int {
	return counts.HTTPErrorCount + counts.StreamErrorCount
}

// MergeOutcomeCounts accumulates an outcome classification into a counter set.
func MergeOutcomeCounts(counts *OutcomeCounts, detail OutcomeDetail) {
	counts.RequestCount++
	switch detail {
	case OutcomeDetailHTTPError:
		counts.HTTPErrorCount++
	case OutcomeDetailStreamError:
		counts.StreamErrorCount++
	case OutcomeDetailClientDisconnected:
		counts.ClientDisconnectedCount++
	default:
		counts.CompletedCount++
	}
}

// ---- Pricing status classifier ----

// PricingStatus is the canonical four-state pricing classification consumed
// by every Observe/Requests surface. It is persisted at materialization time;
// read models must not re-derive it from HTTP status, cost or reasons.
type PricingStatus string

const (
	PricingStatusPriced     PricingStatus = "priced"
	PricingStatusUnpriced   PricingStatus = "unpriced"
	PricingStatusIneligible PricingStatus = "ineligible"
	PricingStatusUnknown    PricingStatus = "unknown"
)

// Canonical unpriced reasons (four fixed keys; sums must equal unpriced).
const (
	UnpricedReasonPricingDisabled        = "PRICING_DISABLED"
	UnpricedReasonMissingTokenUsage      = "MISSING_TOKEN_USAGE"
	UnpricedReasonStreamUsageUnavailable = "STREAM_USAGE_UNAVAILABLE"
	UnpricedReasonMissingPriceData       = "MISSING_PRICE_DATA"
)

// Canonical pricing resolution kinds (only MISSING_PRICE_DATA may carry one).
const (
	PricingResolutionMissingComponent          = "missing_component"
	PricingResolutionCurrencyMigrationRequired = "currency_migration_required"
	PricingResolutionUnsupportedUnit           = "unsupported_unit"
	PricingResolutionSnapshotIncoherent        = "snapshot_incoherent"
)

// PricingEvidenceTrust distinguishes trusted request-time facts from legacy
// untrusted evidence. New rows are always trusted; legacy backfill may be
// untrusted with canonical cost nulled.
type PricingEvidenceTrust string

const (
	PricingEvidenceTrustTrusted         PricingEvidenceTrust = "trusted"
	PricingEvidenceTrustLegacyUntrusted PricingEvidenceTrust = "legacy_untrusted"
)

// ClassifyPricingStatus derives the canonical four-state pricing status at
// materialization time. non-2xx is always ineligible; 2xx with a priced flag
// is priced; 2xx with an unpriced reason is unpriced; 2xx without reliable
// evidence is unknown (only legacy rows may hit this path).
func ClassifyPricingStatus(httpSuccess bool, priced bool, unpricedReason *string, evidenceTrust PricingEvidenceTrust) (PricingStatus, string) {
	if !httpSuccess {
		return PricingStatusIneligible, ""
	}
	if evidenceTrust == PricingEvidenceTrustLegacyUntrusted {
		return PricingStatusUnknown, ""
	}
	if priced {
		return PricingStatusPriced, ""
	}
	if reason := strings.TrimSpace(dereferenceString(unpricedReason)); reason != "" {
		return PricingStatusUnpriced, reason
	}
	return PricingStatusUnknown, ""
}

// CostSegmentKeyFor returns the canonical server-generated cost segment key.
// Identified epochs use e.<epoch>; legacy rows with a valid code use
// l.<AAA>; code missing/invalid uses l.__unknown__.
func CostSegmentKeyFor(reportingCurrencyEpoch *int, legacyCurrencyCode string, legacyCodeValid bool) string {
	if reportingCurrencyEpoch != nil && *reportingCurrencyEpoch > 0 {
		return "e." + itoa(*reportingCurrencyEpoch)
	}
	code := strings.ToUpper(strings.TrimSpace(legacyCurrencyCode))
	if legacyCodeValid && len(code) == 3 {
		return "l." + code
	}
	return "l.__unknown__"
}

// PricingCoverageState derives the canonical coverage state from counts.
//
//	eligible>0 && unpriced==0 && unknown==0          -> complete
//	priced>0 && (unpriced>0 || unknown>0)            -> partial
//	priced==0 && eligible>0                          -> no_trusted_cost
//	eligible==0                                      -> no_eligible
func PricingCoverageState(eligible int, priced int, unpriced int, unknown int) string {
	if eligible <= 0 {
		return "no_eligible"
	}
	if unpriced == 0 && unknown == 0 {
		return "complete"
	}
	if priced > 0 {
		return "partial"
	}
	return "no_trusted_cost"
}

// PricingReconciliation is the four-state + four-reason count block every
// summary, series point, breakdown item, target item, activity cohort, daily
// rollup and export fragment must carry.
type PricingReconciliation struct {
	EligibleRequestCount   int            `json:"pricing_eligible_request_count"`
	IneligibleRequestCount int            `json:"pricing_ineligible_request_count"`
	PricedRequestCount     int            `json:"priced_request_count"`
	UnpricedRequestCount   int            `json:"unpriced_request_count"`
	UnknownRequestCount    int            `json:"pricing_unknown_request_count"`
	UnpricedReasonCounts   map[string]int `json:"unpriced_reason_counts"`
	PricingCoverageState   string         `json:"pricing_coverage_state"`
}

// NewPricingReconciliation initializes the fixed four-reason keys (always
// present, even when zero).
func NewPricingReconciliation() PricingReconciliation {
	return PricingReconciliation{
		UnpricedReasonCounts: map[string]int{
			UnpricedReasonPricingDisabled:        0,
			UnpricedReasonMissingTokenUsage:      0,
			UnpricedReasonStreamUsageUnavailable: 0,
			UnpricedReasonMissingPriceData:       0,
		},
	}
}

// MergePricingReconciliation accumulates one classified request.
func MergePricingReconciliation(reconciliation *PricingReconciliation, status PricingStatus, unpricedReason string) {
	if status == PricingStatusIneligible {
		reconciliation.IneligibleRequestCount++
		return
	}
	reconciliation.EligibleRequestCount++
	switch status {
	case PricingStatusPriced:
		reconciliation.PricedRequestCount++
	case PricingStatusUnknown:
		reconciliation.UnknownRequestCount++
	case PricingStatusUnpriced:
		reconciliation.UnpricedRequestCount++
		reason := strings.TrimSpace(unpricedReason)
		if _, ok := reconciliation.UnpricedReasonCounts[reason]; !ok {
			reason = UnpricedReasonMissingPriceData
		}
		reconciliation.UnpricedReasonCounts[reason]++
	}
}

// FinalizePricingReconciliation computes the coverage state after all merges.
func FinalizePricingReconciliation(reconciliation *PricingReconciliation) {
	reconciliation.PricingCoverageState = PricingCoverageState(
		reconciliation.EligibleRequestCount,
		reconciliation.PricedRequestCount,
		reconciliation.UnpricedRequestCount,
		reconciliation.UnknownRequestCount,
	)
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		buffer[index] = '-'
	}
	return string(buffer[index:])
}
