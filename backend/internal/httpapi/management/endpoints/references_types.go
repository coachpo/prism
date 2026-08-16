package endpoints

import "time"

// endpointReferenceSummary is the canonical four-count read model shared by
// batch, single detail and DELETE. It is derived exclusively from the direct
// reference query; enabled counts never filter deletion blockers.
type endpointReferenceSummary struct {
	DirectReferenceCount  int `json:"direct_reference_count"`
	ReferencingModelCount int `json:"referencing_model_count"`
	EnabledReferenceCount int `json:"enabled_reference_count"`
	OrphanReferenceCount  int `json:"orphan_reference_count"`
}

type endpointReferenceBatchRequest struct {
	EndpointIDs []int `json:"endpoint_ids"`
}

type endpointReferenceBatchItem struct {
	EndpointID int                      `json:"endpoint_id"`
	Summary    endpointReferenceSummary `json:"summary"`
}

type endpointReferenceBatchResponse struct {
	Items []endpointReferenceBatchItem `json:"items"`
}

type endpointReferencePricingTemplate struct {
	ID                int     `json:"id"`
	Name              string  `json:"name"`
	CurrentRevisionID *string `json:"current_revision_id"`
	CurrentVersion    int     `json:"current_version"`
}

type endpointReferenceOwnerModel struct {
	ID                   int     `json:"id"`
	ModelID              string  `json:"model_id"`
	DisplayName          *string `json:"display_name"`
	IsEnabled            bool    `json:"is_enabled"`
	OpenAIAcceptedFormat *string `json:"openai_accepted_format"`
}

type endpointReferenceAccessTarget struct {
	ID        int  `json:"id"`
	Position  int  `json:"position"`
	IsEnabled bool `json:"is_enabled"`
}

// endpointReferenceItem is one direct reference row. Orphan connections keep
// kind="orphan_connection" with null owner/access-target/pricing fields and
// still count as blockers.
type endpointReferenceItem struct {
	Kind                  string                            `json:"kind"`
	ConnectionID          int                               `json:"connection_id"`
	TerminalTargetID      int                               `json:"terminal_target_id"`
	TerminalTargetName    *string                           `json:"terminal_target_name"`
	APIFamily             string                            `json:"api_family"`
	ConnectionIsActive    bool                              `json:"connection_is_active"`
	AccessTarget          *endpointReferenceAccessTarget    `json:"access_target"`
	OwnerModel            *endpointReferenceOwnerModel      `json:"owner_model"`
	OpenAITextCapability  *string                           `json:"openai_text_capability"`
	OpenAIImageCapability *string                           `json:"openai_image_capability"`
	PricingTemplate       *endpointReferencePricingTemplate `json:"pricing_template"`
	Enabled               bool                              `json:"enabled"`
	InactiveReasons       []string                          `json:"inactive_reasons"`
}

type endpointReferencePage struct {
	Items                 []endpointReferenceItem `json:"items"`
	TotalCount            int                     `json:"total_count"`
	NextCursor            *string                 `json:"next_cursor"`
	ReferenceSnapshotHash string                  `json:"reference_snapshot_hash"`
}

type endpointReferenceDetailResponse struct {
	EndpointID    int                      `json:"endpoint_id"`
	Summary       endpointReferenceSummary `json:"summary"`
	ReferencePage endpointReferencePage    `json:"reference_page"`
}

// endpointInUseDetail is the typed DELETE 409 body. It carries the canonical
// summary plus a bounded first page and the references URL so the dialog can
// resume pagination on the same snapshot.
type endpointInUseDetail struct {
	Code          string                   `json:"code"`
	Message       string                   `json:"message"`
	EndpointID    int                      `json:"endpoint_id"`
	Summary       endpointReferenceSummary `json:"summary"`
	ReferencePage endpointReferencePage    `json:"reference_page"`
	ReferencesURL string                   `json:"references_url"`
}

type endpointDeletedResponse struct {
	Deleted bool `json:"deleted"`
}

type orphanCleanupResponse struct {
	Deleted      bool `json:"deleted"`
	ConnectionID int  `json:"connection_id"`
}

// connectionNotOrphanedDetail is the typed 409 body when an orphan cleanup
// races with a new owner attach.
type connectionNotOrphanedDetail struct {
	Code    string                `json:"code"`
	Message string                `json:"message"`
	Item    endpointReferenceItem `json:"item"`
}

// referenceIntegrityErrorDetail is the typed 409 body when duplicate-owner
// corruption is detected. Reads fail closed with affected IDs.
type referenceIntegrityErrorDetail struct {
	Code                  string `json:"code"`
	Message               string `json:"message"`
	EndpointID            int    `json:"endpoint_id"`
	AffectedConnectionIDs []int  `json:"affected_connection_ids"`
}

type endpointVerifyRequest struct {
	APIFamily              string `json:"api_family"`
	ExpectedConfigRevision int64  `json:"expected_config_revision"`
}

type endpointVerifyResponse struct {
	EndpointID        int     `json:"endpoint_id"`
	APIFamily         string  `json:"api_family"`
	ConfigRevision    int64   `json:"config_revision"`
	APIKeyFingerprint *string `json:"api_key_fingerprint"`
	IsCurrent         bool    `json:"is_current"`
	Outcome           string  `json:"outcome"`
	ProbePath         string  `json:"probe_path"`
	UpstreamStatus    *int    `json:"upstream_status"`
	DurationMS        int64   `json:"duration_ms"`
	ErrorSummary      *string `json:"error_summary"`
}

type endpointConfigChangedDetail struct {
	Code     string           `json:"code"`
	Message  string           `json:"message"`
	Endpoint endpointResponse `json:"endpoint"`
}

var _ = time.Time{}
