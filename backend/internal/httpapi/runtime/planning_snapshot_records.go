package runtime

import (
	"database/sql"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

type runtimeModelRecord struct {
	ID                    int
	ProfileID             int
	APIFamily             string
	ModelID               string
	VendorID              *int
	VendorKey             *string
	VendorName            *string
	AuditEnabled          bool
	AuditCaptureBodies    bool
	LoadbalanceStrategyID *int
	OpenAIAcceptedFormat  *string
	OpenAIImageOperations *string
	CreatedAt             time.Time
}

type runtimeEndpoint struct {
	ID      int
	Name    *string
	BaseURL string
}

type runtimePricingTemplateSnapshot struct {
	ID                     int
	Name                   string
	RevisionID             int64
	PricingUnit            string
	PricingCurrencyCode    string
	ReportingCurrencyEpoch *int
	InputPrice             string
	OutputPrice            string
	CachedInputPrice       string
	CacheCreationPrice     string
	ReasoningPrice         string
	TierInputTokensAbove   *int
	TierInputPrice         string
	TierOutputPrice        string
	TierCachedInputPrice   string
	TierCacheCreationPrice string
	TierReasoningPrice     string
	Version                int
	VersionEffectiveAt     *time.Time
}

type runtimeEndpointFXSnapshot struct {
	ModelID    string
	EndpointID int
	FXRate     string
}

type runtimeReportCurrencySnapshot struct {
	Code   string
	Symbol string
	// Epoch is the profile-local reporting currency epoch ordinal (0 when
	// unavailable, e.g. legacy settings without an epoch row).
	Epoch int
}

type runtimeConnectionUpstreamAuthSnapshot struct {
	AuthHeader            string
	AuthValue             string
	ExtraHeaders          map[string]string
	ControlledHeaderNames map[string]struct{}
}

type runtimeConnection struct {
	ID                      int
	ProfileID               int
	APIFamily               string
	ModelConfigID           int
	EndpointID              int
	Priority                int
	QPSLimit                *int
	MaxInFlightNonStream    *int
	MaxInFlightStream       *int
	Name                    *string
	AuthType                *string
	EncryptedEndpointAPIKey string
	CustomHeaders           map[string]any
	CustomRequestParameters *terminaltarget.CustomRequestParameters
	PricingTemplateID       *int
	PricingTemplateSnapshot *runtimePricingTemplateSnapshot
	OpenAITextCapability    *string
	OpenAIImageCapability   *string
	RoutingSchedule         terminaltarget.CompiledRoutingSchedule
	EndpointFXSnapshot      *runtimeEndpointFXSnapshot
	UpstreamAuth            *runtimeConnectionUpstreamAuthSnapshot
	Endpoint                runtimeEndpoint
}

func cloneRuntimeIntPointer(source *int) *int {
	if source == nil {
		return nil
	}
	return intPtr(*source)
}

func cloneRuntimeInt64Pointer(source *int64) *int64 {
	if source == nil {
		return nil
	}
	return int64Ptr(*source)
}

func cloneRuntimeStringPointer(source *string) *string {
	if source == nil {
		return nil
	}
	return stringPtr(*source)
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableFloat64(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	resolved := value.Float64
	return &resolved
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}
