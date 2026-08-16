package settings

// This file owns the bounded, resumable currency-migration draft protocol.
// All live routes below consume the server-side draft contract from the
// Pricing spec; no full-list preview/commit compatibility path is mounted.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

const (
	currencyDraftDefaultChunkLimit = 100
	currencyDraftMaxChunkLimit     = 500
	currencyDraftPreviewLimit      = 50
	currencyDraftMaxPreviewLimit   = 100
	currencyDraftChunkMaxRows      = 100
	currencyDraftTTL               = 24 * time.Hour
)

type currencyMigrationDraftCreateRequest struct {
	DraftID                        string  `json:"draft_id"`
	MigrationOperationID           string  `json:"migration_operation_id"`
	OperationKind                  string  `json:"operation_kind"`
	TargetCurrencyCode             string  `json:"target_currency_code"`
	TargetCurrencySymbol           string  `json:"target_currency_symbol"`
	ExpectedInventoryID            *string `json:"expected_inventory_id"`
	ExpectedInventoryHash          *string `json:"expected_inventory_hash"`
	ExpectedInventoryGeneration    *int64  `json:"expected_inventory_generation"`
	ExpectedReportingCurrencyEpoch *int64  `json:"expected_reporting_currency_epoch"`
	ExpectedSettingsUpdatedAt      string  `json:"expected_settings_updated_at"`
}

type currencyMigrationDraftChunkRowRequest struct {
	TemplateID         int     `json:"template_id"`
	ExpectedVersion    int     `json:"expected_version"`
	ExpectedUpdatedAt  string  `json:"expected_updated_at"`
	InputPrice         *string `json:"input_price"`
	OutputPrice        *string `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
}

// UnmarshalJSON makes the five price keys explicit. In particular, a
// specialty price may be JSON null, but it may not be silently omitted.
func (row *currencyMigrationDraftChunkRowRequest) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	allowed := map[string]struct{}{
		"template_id": {}, "expected_version": {}, "expected_updated_at": {},
		"input_price": {}, "output_price": {}, "cached_input_price": {},
		"cache_creation_price": {}, "reasoning_price": {},
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	for _, key := range []string{"template_id", "expected_version", "expected_updated_at", "input_price", "output_price", "cached_input_price", "cache_creation_price", "reasoning_price"} {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("%s is required", key)
		}
	}
	type plain currencyMigrationDraftChunkRowRequest
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*row = currencyMigrationDraftChunkRowRequest(decoded)
	return nil
}

type currencyMigrationDraftChunkRequest struct {
	Items []currencyMigrationDraftChunkRowRequest `json:"items"`
}

type currencyMigrationDraftPreviewRequest struct {
	OperationKind                  string  `json:"operation_kind"`
	MigrationOperationID           string  `json:"migration_operation_id"`
	DraftID                        string  `json:"draft_id"`
	DraftHash                      string  `json:"draft_hash"`
	ExpectedInventoryID            *string `json:"expected_inventory_id"`
	ExpectedInventoryHash          *string `json:"expected_inventory_hash"`
	ExpectedInventoryGeneration    *int64  `json:"expected_inventory_generation"`
	ExpectedReportingCurrencyEpoch *int64  `json:"expected_reporting_currency_epoch"`
	ExpectedSettingsUpdatedAt      string  `json:"expected_settings_updated_at"`
}

type currencyMigrationDraftCommitRequest struct {
	OperationKind                  string  `json:"operation_kind"`
	MigrationOperationID           string  `json:"migration_operation_id"`
	DraftID                        string  `json:"draft_id"`
	DraftHash                      string  `json:"draft_hash"`
	PreviewHash                    string  `json:"preview_hash"`
	ExpectedInventoryID            *string `json:"expected_inventory_id"`
	ExpectedInventoryHash          *string `json:"expected_inventory_hash"`
	ExpectedInventoryGeneration    *int64  `json:"expected_inventory_generation"`
	ExpectedReportingCurrencyEpoch *int64  `json:"expected_reporting_currency_epoch"`
	ExpectedSettingsUpdatedAt      string  `json:"expected_settings_updated_at"`
}

type currencyMigrationDraftHeaderRow struct {
	DraftID                        string
	ProfileID                      int
	MigrationOperationID           string
	OperationKind                  string
	TargetCurrencyCode             string
	TargetCurrencySymbol           string
	ExpectedInventoryID            *int64
	ExpectedInventoryHash          *string
	ExpectedInventoryGeneration    *int64
	ExpectedReportingCurrencyEpoch *int64
	ExpectedSettingsUpdatedAt      time.Time
	Status                         string
	NormalizedHeaderHash           string
	ReceivedChunkCount             int
	DraftHash                      *string
	TemplateCount                  *int
	CommittedResultOperationID     *string
	CreatedAt                      time.Time
	UpdatedAt                      time.Time
	ExpiresAt                      time.Time
}

type currencyMigrationDraftChunkPage struct {
	Items         []currencyMigrationDraftChunkSummary `json:"items"`
	TotalCount    int                                  `json:"total_count"`
	ConsumedCount int                                  `json:"consumed_count"`
	NextCursor    *string                              `json:"next_cursor"`
}

type currencyMigrationDraftChunkSummary struct {
	Ordinal     int    `json:"ordinal"`
	RowCount    int    `json:"row_count"`
	ContentHash string `json:"content_hash"`
}

type currencyMigrationDraftHeaderResponse struct {
	DraftID                        string                          `json:"draft_id"`
	MigrationOperationID           string                          `json:"migration_operation_id"`
	OperationKind                  string                          `json:"operation_kind"`
	TargetCurrencyCode             string                          `json:"target_currency_code"`
	TargetCurrencySymbol           string                          `json:"target_currency_symbol"`
	ExpectedInventoryID            *string                         `json:"expected_inventory_id"`
	ExpectedInventoryHash          *string                         `json:"expected_inventory_hash"`
	ExpectedInventoryGeneration    *int64                          `json:"expected_inventory_generation"`
	ExpectedReportingCurrencyEpoch *int64                          `json:"expected_reporting_currency_epoch"`
	ExpectedSettingsUpdatedAt      string                          `json:"expected_settings_updated_at"`
	Status                         string                          `json:"status"`
	NormalizedHeaderHash           string                          `json:"normalized_header_hash"`
	ReceivedChunkCount             int                             `json:"received_chunk_count"`
	ChunkPage                      currencyMigrationDraftChunkPage `json:"chunk_page"`
	DraftHash                      *string                         `json:"draft_hash"`
	TemplateCount                  *int                            `json:"template_count"`
	CommittedResultOperationID     *string                         `json:"committed_result_operation_id"`
	CreatedAt                      string                          `json:"created_at"`
	UpdatedAt                      string                          `json:"updated_at"`
}

type currencyMigrationDraftItem struct {
	TemplateID         int     `json:"template_id"`
	ExpectedVersion    int     `json:"expected_version"`
	ExpectedUpdatedAt  string  `json:"expected_updated_at"`
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
	TemplateName       string  `json:"template_name,omitempty"`
	ReferenceCount     int64   `json:"reference_count,omitempty"`
}

type currencyMigrationDraftItemResponse struct {
	TemplateID         int     `json:"template_id"`
	TemplateName       string  `json:"template_name"`
	ExpectedVersion    int     `json:"expected_version"`
	ExpectedUpdatedAt  string  `json:"expected_updated_at"`
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
	ReferenceCount     int64   `json:"reference_count"`
}

type currencyMigrationDraftItemPage struct {
	Items      []currencyMigrationDraftItemResponse `json:"items"`
	TotalCount int                                  `json:"total_count"`
	NextCursor *string                              `json:"next_cursor"`
	HasMore    bool                                 `json:"has_more"`
	Limit      int                                  `json:"limit"`
}

type currencyMigrationPreviewItem struct {
	TemplateID                int     `json:"template_id"`
	Name                      string  `json:"name"`
	CurrentVersion            int     `json:"current_version"`
	NextVersion               int     `json:"next_version"`
	CurrentInputPrice         *string `json:"current_input_price"`
	CurrentOutputPrice        *string `json:"current_output_price"`
	CurrentCachedInputPrice   *string `json:"current_cached_input_price"`
	CurrentCacheCreationPrice *string `json:"current_cache_creation_price"`
	CurrentReasoningPrice     *string `json:"current_reasoning_price"`
	NewInputPrice             string  `json:"new_input_price"`
	NewOutputPrice            string  `json:"new_output_price"`
	NewCachedInputPrice       *string `json:"new_cached_input_price"`
	NewCacheCreationPrice     *string `json:"new_cache_creation_price"`
	NewReasoningPrice         *string `json:"new_reasoning_price"`
	ReferenceCount            int64   `json:"reference_count"`
}

type currencyMigrationPreviewPage struct {
	Items      []currencyMigrationPreviewItem `json:"items"`
	TotalCount int                            `json:"total_count"`
	NextCursor *string                        `json:"next_cursor"`
	HasMore    bool                           `json:"has_more"`
	Limit      int                            `json:"limit"`
}

type currencyMigrationDraftPreviewResponse struct {
	OperationKind           string                          `json:"operation_kind"`
	MigrationOperationID    string                          `json:"migration_operation_id"`
	DraftID                 string                          `json:"draft_id"`
	DraftHash               string                          `json:"draft_hash"`
	PreviewHash             string                          `json:"preview_hash"`
	TargetCurrencyCode      string                          `json:"target_currency_code"`
	TargetCurrencySymbol    string                          `json:"target_currency_symbol"`
	CurrentCurrencyCode     *string                         `json:"current_currency_code"`
	CurrentEpoch            *int                            `json:"current_epoch"`
	NextEpoch               *int                            `json:"next_epoch"`
	TemplateCount           int                             `json:"template_count"`
	RevisionChangeCount     int                             `json:"revision_change_count"`
	TemplatePage            currencyMigrationPreviewPage    `json:"template_page"`
	Committable             bool                            `json:"committable"`
	ValidationErrors        []map[string]any                `json:"validation_errors"`
	EpochChange             bool                            `json:"epoch_change"`
	InventoryID             *string                         `json:"inventory_id,omitempty"`
	InventoryHash           *string                         `json:"inventory_hash,omitempty"`
	ArchivedFXEvidenceCount int                             `json:"archived_fx_evidence_count,omitempty"`
	FXEvidencePage          *pricingMigrationFXEvidencePage `json:"fx_evidence_page,omitempty"`
}

type currencyMigrationPreviewItemsResponse struct {
	PreviewHash string                       `json:"preview_hash"`
	Page        currencyMigrationPreviewPage `json:"page"`
}

type currencyDraftCursor struct {
	Version     int    `json:"v"`
	ProfileID   int    `json:"profile"`
	DraftID     string `json:"draft"`
	Kind        string `json:"kind"`
	Binding     string `json:"binding"`
	LastOrdinal int    `json:"last_ordinal,omitempty"`
	LastID      int    `json:"last_id,omitempty"`
}

type currencyDraftAuthoritativeTemplate struct {
	ID                 int
	Name               string
	Version            int
	RevisionID         *int64
	LegacyEvidenceID   *int64
	UpdatedAt          time.Time
	InputPrice         *string
	OutputPrice        *string
	CachedInputPrice   *string
	CacheCreationPrice *string
	ReasoningPrice     *string
	ReferenceCount     int64
}

func (s *Service) handleCreateCurrencyMigrationDraft(w http.ResponseWriter, r *http.Request) {
	var request currencyMigrationDraftCreateRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "Invalid request body"})
		return
	}
	response, err := pgxutil.InRepeatableReadWriteTxValue(r.Context(), s.pool, "currency migration draft create", func(tx pgx.Tx) (currencyMigrationDraftHeaderResponse, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return currencyMigrationDraftHeaderResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "currency_migration_owner_unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		if err := lockCurrencyProfile(r.Context(), tx, profile.ID); err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		header, headerHash, err := normalizeCurrencyDraftHeader(request)
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		settingsRow, found, err := loadUserSettings(r.Context(), tx, profile.ID, true)
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		if !found {
			settingsRow, err = insertDefaultUserSettings(r.Context(), tx, profile.ID, s.nowUTC())
			if err != nil {
				return currencyMigrationDraftHeaderResponse{}, err
			}
		}
		if err := validateCurrencyDraftRequestAgainstSettings(header, settingsRow, r.Context(), tx); err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		if existing, ok, err := loadCurrencyDraftByOperation(r.Context(), tx, profile.ID, header.MigrationOperationID, false); err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		} else if ok {
			if existing.NormalizedHeaderHash != headerHash || existing.DraftID != header.DraftID {
				return currencyMigrationDraftHeaderResponse{}, currencyMigrationOperationConflict()
			}
			return s.currencyDraftHeaderResponse(r.Context(), tx, existing)
		}
		if err := claimCurrencyMigrationReservation(r.Context(), tx, profile.ID, header.MigrationOperationID, header.OperationKind, headerHash, s.nowUTC()); err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO pricing_currency_migration_drafts (
			draft_id, profile_id, migration_operation_id, operation_kind, target_currency_code, target_currency_symbol,
			expected_inventory_id, expected_inventory_hash, expected_inventory_generation, expected_reporting_currency_epoch,
			expected_settings_updated_at, status, normalized_header_hash, received_chunk_count, created_at, updated_at, expires_at
		) VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7::bigint, $8, $9, $10, $11, 'uploading', $12, 0, $13, $13, $14)`,
			header.DraftID, profile.ID, header.MigrationOperationID, header.OperationKind, header.TargetCurrencyCode, header.TargetCurrencySymbol,
			header.ExpectedInventoryID, header.ExpectedInventoryHash, header.ExpectedInventoryGeneration, header.ExpectedReportingCurrencyEpoch,
			header.ExpectedSettingsUpdatedAt, headerHash, s.nowUTC(), s.nowUTC().Add(currencyDraftTTL)); err != nil {
			return currencyMigrationDraftHeaderResponse{}, fmt.Errorf("create currency migration draft: %w", err)
		}
		created, ok, err := loadCurrencyDraftByID(r.Context(), tx, profile.ID, header.DraftID, false)
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		if !ok {
			return currencyMigrationDraftHeaderResponse{}, fmt.Errorf("currency migration draft disappeared after create")
		}
		return s.currencyDraftHeaderResponse(r.Context(), tx, created)
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusCreated, response)
}

func (s *Service) handleGetCurrencyMigrationDraft(w http.ResponseWriter, r *http.Request) {
	draftID, err := normalizeUUIDV4(chi.URLParam(r, "draft_id"))
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "draft_id must be a UUIDv4"})
		return
	}
	response, err := pgxutil.InRepeatableReadWriteTxValue(r.Context(), s.pool, "currency migration draft read", func(tx pgx.Tx) (currencyMigrationDraftHeaderResponse, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return currencyMigrationDraftHeaderResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "currency_migration_owner_unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		header, ok, err := loadCurrencyDraftByID(r.Context(), tx, profile.ID, draftID, true)
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		if !ok {
			return currencyMigrationDraftHeaderResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "currency migration draft not found"}
		}
		if header.Status == "uploading" && s.nowUTC().After(header.ExpiresAt) {
			if _, err := tx.Exec(r.Context(), `UPDATE pricing_currency_migration_drafts SET status = 'expired', updated_at = $2 WHERE draft_id = $1::uuid`, header.DraftID, s.nowUTC()); err != nil {
				return currencyMigrationDraftHeaderResponse{}, err
			}
			header.Status = "expired"
			header.UpdatedAt = s.nowUTC()
		}
		return s.currencyDraftHeaderResponse(r.Context(), tx, header)
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutCurrencyMigrationDraftChunk(w http.ResponseWriter, r *http.Request) {
	draftID, err := normalizeUUIDV4(chi.URLParam(r, "draft_id"))
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "draft_id must be a UUIDv4"})
		return
	}
	ordinal, err := strconv.Atoi(chi.URLParam(r, "ordinal"))
	if err != nil || ordinal < 1 {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "ordinal must be a positive integer"})
		return
	}
	var request currencyMigrationDraftChunkRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "Invalid request body"})
		return
	}
	normalizedRows, contentHash, err := normalizeCurrencyDraftChunk(request.Items)
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InRepeatableReadWriteTxValue(r.Context(), s.pool, "currency migration draft chunk", func(tx pgx.Tx) (currencyMigrationDraftHeaderResponse, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return currencyMigrationDraftHeaderResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "currency_migration_owner_unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		header, ok, err := loadCurrencyDraftByID(r.Context(), tx, profile.ID, draftID, true)
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		if !ok {
			return currencyMigrationDraftHeaderResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "currency migration draft not found"}
		}
		if header.Status != "uploading" {
			return currencyMigrationDraftHeaderResponse{}, currencyMigrationDraftStateError(header.Status)
		}
		var existingHash string
		var existingCount int
		err = tx.QueryRow(r.Context(), `SELECT content_hash, row_count FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid AND ordinal = $2`, draftID, ordinal).Scan(&existingHash, &existingCount)
		switch {
		case err == nil:
			if existingHash != contentHash || existingCount != len(normalizedRows) {
				return currencyMigrationDraftHeaderResponse{}, currencyMigrationDraftConflict()
			}
		case err != pgx.ErrNoRows:
			return currencyMigrationDraftHeaderResponse{}, err
		default:
			payload, marshalErr := json.Marshal(normalizedRows)
			if marshalErr != nil {
				return currencyMigrationDraftHeaderResponse{}, marshalErr
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO pricing_currency_migration_draft_chunks (draft_id, ordinal, row_count, content_hash, created_at, payload) VALUES ($1::uuid, $2, $3, $4, $5, $6::jsonb)`, draftID, ordinal, len(normalizedRows), contentHash, s.nowUTC(), payload); err != nil {
				return currencyMigrationDraftHeaderResponse{}, fmt.Errorf("store currency migration draft chunk: %w", err)
			}
		}
		if _, err := tx.Exec(r.Context(), `UPDATE pricing_currency_migration_drafts SET received_chunk_count = (SELECT count(*) FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid), updated_at = $2 WHERE draft_id = $1::uuid`, draftID, s.nowUTC()); err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		updated, ok, err := loadCurrencyDraftByID(r.Context(), tx, profile.ID, draftID, false)
		if err != nil || !ok {
			if err != nil {
				return currencyMigrationDraftHeaderResponse{}, err
			}
			return currencyMigrationDraftHeaderResponse{}, fmt.Errorf("currency migration draft disappeared after chunk write")
		}
		return s.currencyDraftHeaderResponse(r.Context(), tx, updated)
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) handleListCurrencyMigrationDraftChunks(w http.ResponseWriter, r *http.Request) {
	s.handleListCurrencyMigrationDraftChunkPage(w, r, false)
}

func (s *Service) handleListCurrencyMigrationDraftItems(w http.ResponseWriter, r *http.Request) {
	s.handleListCurrencyMigrationDraftItemPage(w, r, false)
}

func (s *Service) handleListCurrencyMigrationDraftPreviewItems(w http.ResponseWriter, r *http.Request) {
	s.handleListCurrencyMigrationDraftItemPage(w, r, true)
}

func (s *Service) handleListCurrencyMigrationDraftChunkPage(w http.ResponseWriter, r *http.Request, _ bool) {
	draftID, err := normalizeUUIDV4(chi.URLParam(r, "draft_id"))
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "draft_id must be a UUIDv4"})
		return
	}
	limit, err := currencyDraftPageLimit(r, currencyDraftDefaultChunkLimit, currencyDraftMaxChunkLimit)
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "currency migration draft chunks", func(tx pgx.Tx) (currencyMigrationDraftChunkPage, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return currencyMigrationDraftChunkPage{}, err
		}
		header, ok, err := loadCurrencyDraftByID(r.Context(), tx, profile.ID, draftID, false)
		if err != nil || !ok {
			if err != nil {
				return currencyMigrationDraftChunkPage{}, err
			}
			return currencyMigrationDraftChunkPage{}, &domainError{StatusCode: http.StatusNotFound, Detail: "currency migration draft not found"}
		}
		cursor, err := s.decodeCurrencyDraftCursor(r.URL.Query().Get("cursor"), currencyDraftCursor{ProfileID: profile.ID, DraftID: draftID, Kind: "chunks", Binding: header.NormalizedHeaderHash})
		if err != nil {
			return currencyMigrationDraftChunkPage{}, err
		}
		return s.loadCurrencyDraftChunkPage(r.Context(), tx, header, profile.ID, limit, cursor.LastOrdinal)
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) handleListCurrencyMigrationDraftItemPage(w http.ResponseWriter, r *http.Request, preview bool) {
	draftID, err := normalizeUUIDV4(chi.URLParam(r, "draft_id"))
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "draft_id must be a UUIDv4"})
		return
	}
	limit, err := currencyDraftPageLimit(r, currencyDraftDefaultChunkLimit, currencyDraftMaxChunkLimit)
	if preview {
		limit, err = currencyDraftPageLimit(r, currencyDraftPreviewLimit, currencyDraftMaxPreviewLimit)
	}
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "currency migration draft items", func(tx pgx.Tx) (any, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		header, ok, err := loadCurrencyDraftByID(r.Context(), tx, profile.ID, draftID, false)
		if err != nil || !ok {
			if err != nil {
				return nil, err
			}
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "currency migration draft not found"}
		}
		if header.Status != "sealed" && header.Status != "committed" {
			return nil, currencyMigrationDraftStateError(header.Status)
		}
		binding := stringValue(header.DraftHash)
		if preview {
			requestedHash := strings.TrimSpace(r.URL.Query().Get("preview_hash"))
			if requestedHash == "" {
				return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "preview_hash is required"}
			}
			computed, err := buildCurrencyMigrationPreview(r.Context(), tx, header, profile.ID)
			if err != nil {
				return nil, err
			}
			if requestedHash != computed.PreviewHash {
				return nil, currencyMigrationPreviewStale()
			}
			binding = computed.PreviewHash
			cursor, err := s.decodeCurrencyDraftCursor(r.URL.Query().Get("cursor"), currencyDraftCursor{ProfileID: profile.ID, DraftID: draftID, Kind: "preview-items", Binding: binding})
			if err != nil {
				return nil, err
			}
			page, err := loadCurrencyMigrationPreviewPage(computed.Items, limit, cursor.LastID)
			if err != nil {
				return nil, err
			}
			if page.page.NextCursor != nil {
				cursorValue := s.encodeCurrencyDraftCursor(currencyDraftCursor{Version: 1, ProfileID: profile.ID, DraftID: draftID, Kind: "preview-items", Binding: binding, LastID: page.lastID})
				page.page.NextCursor = &cursorValue
			}
			return currencyMigrationPreviewItemsResponse{PreviewHash: computed.PreviewHash, Page: page.page}, nil
		}
		cursor, err := s.decodeCurrencyDraftCursor(r.URL.Query().Get("cursor"), currencyDraftCursor{ProfileID: profile.ID, DraftID: draftID, Kind: "items", Binding: binding})
		if err != nil {
			return nil, err
		}
		return s.loadCurrencyDraftItemPage(r.Context(), tx, header, profile.ID, limit, cursor.LastID)
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) handleSealCurrencyMigrationDraft(w http.ResponseWriter, r *http.Request) {
	draftID, err := normalizeUUIDV4(chi.URLParam(r, "draft_id"))
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "draft_id must be a UUIDv4"})
		return
	}
	response, err := pgxutil.InRepeatableReadWriteTxValue(r.Context(), s.pool, "currency migration draft seal", func(tx pgx.Tx) (currencyMigrationDraftHeaderResponse, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return currencyMigrationDraftHeaderResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "currency_migration_owner_unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		if err := lockCurrencyProfile(r.Context(), tx, profile.ID); err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		header, ok, err := loadCurrencyDraftByID(r.Context(), tx, profile.ID, draftID, true)
		if err != nil || !ok {
			if err != nil {
				return currencyMigrationDraftHeaderResponse{}, err
			}
			return currencyMigrationDraftHeaderResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "currency migration draft not found"}
		}
		if header.Status == "sealed" || header.Status == "committed" {
			return s.currencyDraftHeaderResponse(r.Context(), tx, header)
		}
		if header.Status != "uploading" {
			return currencyMigrationDraftHeaderResponse{}, currencyMigrationDraftStateError(header.Status)
		}
		if s.nowUTC().After(header.ExpiresAt) {
			return currencyMigrationDraftHeaderResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_expired"}
		}
		authoritative, err := loadCurrencyDraftAuthoritativeTemplates(r.Context(), tx, profile.ID, true)
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		rows, err := loadCurrencyDraftPayloadRows(r.Context(), tx, draftID)
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		items, err := sealCurrencyDraftItems(authoritative, rows)
		if err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		for _, item := range items {
			parsed, parseErr := time.Parse(time.RFC3339Nano, item.ExpectedUpdatedAt)
			if parseErr != nil {
				return currencyMigrationDraftHeaderResponse{}, parseErr
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO pricing_currency_migration_draft_items (draft_id, template_id, expected_version, expected_updated_at, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, template_name, reference_count) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, draftID, item.TemplateID, item.ExpectedVersion, parsed, item.InputPrice, item.OutputPrice, item.CachedInputPrice, item.CacheCreationPrice, item.ReasoningPrice, item.TemplateName, item.ReferenceCount); err != nil {
				return currencyMigrationDraftHeaderResponse{}, fmt.Errorf("store sealed currency migration item: %w", err)
			}
		}
		draftHash := hashCanonicalCurrencyDraft(header.NormalizedHeaderHash, items)
		if _, err := tx.Exec(r.Context(), `UPDATE pricing_currency_migration_drafts SET status = 'sealed', draft_hash = $2, template_count = $3, updated_at = $4 WHERE draft_id = $1::uuid`, draftID, draftHash, len(items), s.nowUTC()); err != nil {
			return currencyMigrationDraftHeaderResponse{}, err
		}
		sealed, ok, err := loadCurrencyDraftByID(r.Context(), tx, profile.ID, draftID, false)
		if err != nil || !ok {
			if err != nil {
				return currencyMigrationDraftHeaderResponse{}, err
			}
			return currencyMigrationDraftHeaderResponse{}, fmt.Errorf("currency migration draft disappeared after seal")
		}
		return s.currencyDraftHeaderResponse(r.Context(), tx, sealed)
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) handleCurrencyMigrationDraftPreview(w http.ResponseWriter, r *http.Request) {
	var request currencyMigrationDraftPreviewRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "Invalid request body"})
		return
	}
	operationID, err := normalizeUUIDV4(request.MigrationOperationID)
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "migration_operation_id must be a UUIDv4"})
		return
	}
	request.MigrationOperationID = operationID
	request.OperationKind = strings.TrimSpace(request.OperationKind)
	if request.OperationKind == "archive_unused_fx" {
		s.handleCurrencyMigrationArchivePreview(w, r, request)
		return
	}
	if request.ExpectedInventoryID != nil || request.ExpectedInventoryHash != nil || request.ExpectedInventoryGeneration != nil || request.ExpectedReportingCurrencyEpoch != nil || request.ExpectedSettingsUpdatedAt != "" {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "unknown_field: archive fields are only accepted for archive_unused_fx"})
		return
	}
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "currency migration preview", func(tx pgx.Tx) (currencyMigrationDraftPreviewResponse, error) {
		if err := auditdomain.CheckAffectedWriterAdmission(r.Context(), tx); err != nil {
			return currencyMigrationDraftPreviewResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "currency_migration_owner_unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return currencyMigrationDraftPreviewResponse{}, err
		}
		draftID, err := normalizeUUIDV4(request.DraftID)
		if err != nil {
			return currencyMigrationDraftPreviewResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "draft_id must be a UUIDv4"}
		}
		header, ok, err := loadCurrencyDraftByID(r.Context(), tx, profile.ID, draftID, false)
		if err != nil || !ok {
			if err != nil {
				return currencyMigrationDraftPreviewResponse{}, err
			}
			return currencyMigrationDraftPreviewResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "currency migration draft not found"}
		}
		if header.MigrationOperationID != request.MigrationOperationID || header.OperationKind != request.OperationKind || stringValue(header.DraftHash) != strings.TrimSpace(request.DraftHash) {
			return currencyMigrationDraftPreviewResponse{}, currencyMigrationDraftConflict()
		}
		computed, err := buildCurrencyMigrationPreview(r.Context(), tx, header, profile.ID)
		if err != nil {
			return currencyMigrationDraftPreviewResponse{}, err
		}
		computed.Response.TemplatePage = firstCurrencyMigrationPreviewPage(computed.Items, currencyDraftPreviewLimit, profile.ID, draftID, computed.PreviewHash, s)
		return computed.Response, nil
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) handleCurrencyMigrationDraftCommit(w http.ResponseWriter, r *http.Request) {
	var request currencyMigrationDraftCommitRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "Invalid request body"})
		return
	}
	request.OperationKind = strings.TrimSpace(request.OperationKind)
	if request.OperationKind == "archive_unused_fx" {
		s.handleCurrencyMigrationArchiveCommit(w, r, request)
		return
	}
	if request.ExpectedInventoryID != nil || request.ExpectedInventoryHash != nil || request.ExpectedInventoryGeneration != nil || request.ExpectedReportingCurrencyEpoch != nil || request.ExpectedSettingsUpdatedAt != "" {
		writeSettingsDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "unknown_field: archive fields are only accepted for archive_unused_fx"})
		return
	}
	response, err := pgxutil.InRepeatableReadWriteTxValue(r.Context(), s.pool, "currency migration commit", func(tx pgx.Tx) (currencyMigrationCommitResponse, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return currencyMigrationCommitResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "currency_migration_owner_unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		draftID, err := normalizeUUIDV4(request.DraftID)
		if err != nil {
			return currencyMigrationCommitResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "draft_id must be a UUIDv4"}
		}
		operationID, err := normalizeUUIDV4(request.MigrationOperationID)
		if err != nil {
			return currencyMigrationCommitResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "migration_operation_id must be a UUIDv4"}
		}
		if existing, ok, err := loadCurrencyMigrationResult(r.Context(), tx, operationID); err != nil {
			return currencyMigrationCommitResponse{}, err
		} else if ok {
			var oldKind, oldHash, oldPreview string
			if err := tx.QueryRow(r.Context(), `SELECT result_kind, normalized_payload_hash, preview_hash FROM pricing_mutation_operations WHERE operation_id = $1::uuid`, operationID).Scan(&oldKind, &oldHash, &oldPreview); err != nil {
				return currencyMigrationCommitResponse{}, err
			}
			if oldKind != request.OperationKind || oldHash != strings.TrimSpace(request.DraftHash) || oldPreview != strings.TrimSpace(request.PreviewHash) {
				return currencyMigrationCommitResponse{}, currencyMigrationOperationConflict()
			}
			return existing, nil
		}
		if err := lockCurrencyProfile(r.Context(), tx, profile.ID); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		header, ok, err := loadCurrencyDraftByID(r.Context(), tx, profile.ID, draftID, true)
		if err != nil || !ok {
			if err != nil {
				return currencyMigrationCommitResponse{}, err
			}
			return currencyMigrationCommitResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "currency migration draft not found"}
		}
		if header.MigrationOperationID != operationID || header.OperationKind != request.OperationKind || stringValue(header.DraftHash) != strings.TrimSpace(request.DraftHash) {
			return currencyMigrationCommitResponse{}, currencyMigrationDraftConflict()
		}
		if header.Status == "committed" {
			if result, ok, err := loadCurrencyMigrationResult(r.Context(), tx, operationID); err != nil {
				return currencyMigrationCommitResponse{}, err
			} else if ok {
				return result, nil
			}
		}
		if header.Status != "sealed" {
			return currencyMigrationCommitResponse{}, currencyMigrationDraftStateError(header.Status)
		}
		settingsRow, found, err := loadUserSettings(r.Context(), tx, profile.ID, true)
		if err != nil || !found {
			if err != nil {
				return currencyMigrationCommitResponse{}, err
			}
			return currencyMigrationCommitResponse{}, fmt.Errorf("currency settings are missing")
		}
		if err := validateCurrencyDraftHeaderAgainstSettings(header, settingsRow, r.Context(), tx); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		computed, err := buildCurrencyMigrationPreviewWithSettings(r.Context(), tx, header, profile.ID, settingsRow, true)
		if err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		if computed.PreviewHash != strings.TrimSpace(request.PreviewHash) {
			return currencyMigrationCommitResponse{}, currencyMigrationPreviewStale()
		}
		authoritative, err := loadCurrencyDraftAuthoritativeTemplates(r.Context(), tx, profile.ID, true)
		if err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		items, err := loadCurrencyDraftItems(r.Context(), tx, draftID)
		if err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		return applyCurrencyMigrationDraftCutover(r.Context(), tx, profile.ID, settingsRow, header, operationID, computed.PreviewHash, authoritative, items, s.nowUTC())
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func lockCurrencyProfile(ctx context.Context, tx pgx.Tx, profileID int) error {
	if err := tx.QueryRow(ctx, `SELECT id FROM profiles WHERE id = $1 FOR UPDATE`, profileID).Scan(new(int)); err != nil {
		return fmt.Errorf("lock profile %d for currency migration: %w", profileID, err)
	}
	return nil
}

func normalizeCurrencyDraftHeader(request currencyMigrationDraftCreateRequest) (currencyMigrationDraftCreateRequest, string, error) {
	draftID, err := normalizeUUIDV4(request.DraftID)
	if err != nil {
		return request, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "draft_id must be a UUIDv4"}
	}
	operationID, err := normalizeUUIDV4(request.MigrationOperationID)
	if err != nil {
		return request, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "migration_operation_id must be a UUIDv4"}
	}
	request.DraftID = draftID
	request.MigrationOperationID = operationID
	request.OperationKind = strings.TrimSpace(request.OperationKind)
	if request.OperationKind != "currency_cutover" && request.OperationKind != "repair_same_currency" {
		return request, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "operation_kind must be currency_cutover or repair_same_currency"}
	}
	code, symbol, err := validateMigrationTargetForDraft(request.TargetCurrencyCode, request.TargetCurrencySymbol)
	if err != nil {
		return request, "", err
	}
	request.TargetCurrencyCode, request.TargetCurrencySymbol = code, symbol
	parsedUpdatedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(request.ExpectedSettingsUpdatedAt))
	if err != nil {
		return request, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "expected_settings_updated_at must be a valid RFC3339 timestamp"}
	}
	request.ExpectedSettingsUpdatedAt = parsedUpdatedAt.UTC().Format(time.RFC3339)
	if request.ExpectedInventoryID == nil && (request.ExpectedInventoryHash != nil || request.ExpectedInventoryGeneration != nil) {
		return request, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "expected inventory id, hash and generation must be all null or all present"}
	}
	if request.ExpectedInventoryID != nil {
		inventoryID, parseErr := canonicalPositiveDecimal(*request.ExpectedInventoryID)
		if parseErr != nil || request.ExpectedInventoryHash == nil || request.ExpectedInventoryGeneration == nil || *request.ExpectedInventoryGeneration < 1 {
			return request, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "expected inventory id, hash and generation must be all present and valid"}
		}
		request.ExpectedInventoryID = &inventoryID
		if strings.TrimSpace(*request.ExpectedInventoryHash) == "" {
			return request, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "expected_inventory_hash must not be empty"}
		}
	}
	canonical, _ := json.Marshal(request)
	sum := sha256.Sum256(canonical)
	return request, hex.EncodeToString(sum[:]), nil
}

func validateMigrationTargetForDraft(code string, symbol string) (string, string, error) {
	canonicalCode := canonicalCurrencyCode(code)
	if canonicalCode == "" {
		return "", "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "target_currency_code must be a canonical 3-letter uppercase ISO code"}
	}
	canonicalSymbol, valid := canonicalCurrencySymbol(symbol)
	if !valid {
		return "", "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "target_currency_symbol must be a canonical symbol"}
	}
	return canonicalCode, canonicalSymbol, nil
}

func validateCurrencyDraftRequestAgainstSettings(header currencyMigrationDraftCreateRequest, settingsRow userSettingsRow, ctx context.Context, tx pgx.Tx) error {
	return validateCurrencyDraftValues(header.ExpectedSettingsUpdatedAt, header.ExpectedReportingCurrencyEpoch, header.ExpectedInventoryID, header.ExpectedInventoryHash, header.ExpectedInventoryGeneration, header.OperationKind, header.TargetCurrencyCode, settingsRow, ctx, tx)
}

func validateCurrencyDraftHeaderAgainstSettings(header currencyMigrationDraftHeaderRow, settingsRow userSettingsRow, ctx context.Context, tx pgx.Tx) error {
	return validateCurrencyDraftValues(header.ExpectedSettingsUpdatedAt.UTC().Format(time.RFC3339), header.ExpectedReportingCurrencyEpoch, positiveDecimalPtr(header.ExpectedInventoryID), header.ExpectedInventoryHash, header.ExpectedInventoryGeneration, header.OperationKind, header.TargetCurrencyCode, settingsRow, ctx, tx)
}

func validateCurrencyDraftValues(expectedUpdatedAt string, expectedEpoch *int64, expectedInventoryID *string, expectedInventoryHash *string, expectedInventoryGeneration *int64, operationKind, targetCode string, settingsRow userSettingsRow, ctx context.Context, tx pgx.Tx) error {
	parsed, err := time.Parse(time.RFC3339, expectedUpdatedAt)
	// The costing CAS wire uses RFC3339 second precision. Compare the same
	// canonical instant rather than the database's sub-second storage value;
	// otherwise a freshly returned GET token can never authorize its first
	// migration draft.
	if err != nil || settingsRow.UpdatedAt.UTC().Truncate(time.Second) != parsed.UTC().Truncate(time.Second) {
		return &domainError{StatusCode: http.StatusConflict, Detail: costingSettingsChangedDetail{CurrentUpdatedAt: settingsRow.UpdatedAt.UTC().Format(time.RFC3339)}}
	}
	currentEpoch, err := loadCurrencyMigrationEpochOnly(ctx, tx, settingsRow)
	if err != nil {
		return err
	}
	if (expectedEpoch == nil) != (currentEpoch == nil) || (expectedEpoch != nil && currentEpoch != nil && *expectedEpoch != int64(*currentEpoch)) {
		return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_stale: reporting currency epoch changed"}
	}
	if (expectedInventoryID == nil) != (expectedInventoryHash == nil) || (expectedInventoryID == nil) != (expectedInventoryGeneration == nil) {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "expected inventory id, hash and generation must be all null or all present"}
	}
	if expectedInventoryID != nil {
		if (currentEpoch == nil && operationKind != "currency_cutover") || (currentEpoch != nil && operationKind != "repair_same_currency") {
			return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_conflict: inventory binding does not match the current migration state"}
		}
		if err := validateCurrencyMigrationInventory(ctx, tx, settingsRow.ProfileID, *expectedInventoryID, *expectedInventoryHash, *expectedInventoryGeneration); err != nil {
			return err
		}
	}
	if operationKind == "currency_cutover" && currentEpoch != nil && targetCode == settingsRow.ReportCurrencyCode {
		return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_required: target currency must differ from the current reporting currency"}
	}
	if operationKind == "repair_same_currency" && (currentEpoch == nil || expectedInventoryID == nil || settingsRow.PricingMigrationState == "ready" || targetCode != settingsRow.ReportCurrencyCode) {
		return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_invalid_kind: same-currency repair must retain the current reporting currency"}
	}
	return nil
}

func claimCurrencyMigrationReservation(ctx context.Context, tx pgx.Tx, profileID int, operationID, kind, hash string, now time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO pricing_mutation_operation_reservations (operation_id, profile_id, intended_result_kind, normalized_identity_hash, created_at) VALUES ($1::uuid, $2, $3, $4, $5) ON CONFLICT (operation_id) DO NOTHING`, operationID, profileID, kind, hash, now); err != nil {
		return fmt.Errorf("reserve currency migration operation: %w", err)
	}
	var existingProfile int
	var existingKind, existingHash string
	if err := tx.QueryRow(ctx, `SELECT profile_id, intended_result_kind, normalized_identity_hash FROM pricing_mutation_operation_reservations WHERE operation_id = $1::uuid`, operationID).Scan(&existingProfile, &existingKind, &existingHash); err != nil {
		return err
	}
	if existingProfile != profileID || existingKind != kind || existingHash != hash {
		return currencyMigrationOperationConflict()
	}
	return nil
}

func loadCurrencyDraftByID(ctx context.Context, tx pgx.Tx, profileID int, draftID string, forUpdate bool) (currencyMigrationDraftHeaderRow, bool, error) {
	return loadCurrencyDraft(ctx, tx, `draft_id = $2::uuid`, profileID, draftID, forUpdate)
}

func loadCurrencyDraftByOperation(ctx context.Context, tx pgx.Tx, profileID int, operationID string, forUpdate bool) (currencyMigrationDraftHeaderRow, bool, error) {
	return loadCurrencyDraft(ctx, tx, `migration_operation_id = $2::uuid`, profileID, operationID, forUpdate)
}

func loadCurrencyDraft(ctx context.Context, tx pgx.Tx, predicate string, profileID int, value string, forUpdate bool) (currencyMigrationDraftHeaderRow, bool, error) {
	query := `SELECT draft_id::text, profile_id, migration_operation_id::text, operation_kind, target_currency_code, target_currency_symbol,
		expected_inventory_id, expected_inventory_hash, expected_inventory_generation, expected_reporting_currency_epoch,
		expected_settings_updated_at, status, normalized_header_hash, received_chunk_count, draft_hash, template_count,
		committed_result_operation_id::text, created_at, updated_at, expires_at
		FROM pricing_currency_migration_drafts WHERE profile_id = $1 AND ` + predicate
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var row currencyMigrationDraftHeaderRow
	var inventoryID, inventoryGeneration, epoch *int64
	var inventoryHash, draftHash, committedID *string
	err := tx.QueryRow(ctx, query, profileID, value).Scan(
		&row.DraftID, &row.ProfileID, &row.MigrationOperationID, &row.OperationKind, &row.TargetCurrencyCode, &row.TargetCurrencySymbol,
		&inventoryID, &inventoryHash, &inventoryGeneration, &epoch, &row.ExpectedSettingsUpdatedAt, &row.Status, &row.NormalizedHeaderHash,
		&row.ReceivedChunkCount, &draftHash, &row.TemplateCount, &committedID, &row.CreatedAt, &row.UpdatedAt, &row.ExpiresAt,
	)
	if err == pgx.ErrNoRows {
		return currencyMigrationDraftHeaderRow{}, false, nil
	}
	if err != nil {
		return currencyMigrationDraftHeaderRow{}, false, fmt.Errorf("load currency migration draft: %w", err)
	}
	row.ExpectedInventoryID, row.ExpectedInventoryHash, row.ExpectedInventoryGeneration, row.ExpectedReportingCurrencyEpoch = inventoryID, inventoryHash, inventoryGeneration, epoch
	row.DraftHash, row.CommittedResultOperationID = draftHash, committedID
	return row, true, nil
}

func (s *Service) currencyDraftHeaderResponse(ctx context.Context, tx pgx.Tx, row currencyMigrationDraftHeaderRow) (currencyMigrationDraftHeaderResponse, error) {
	page, err := s.loadCurrencyDraftChunkPage(ctx, tx, row, row.ProfileID, currencyDraftDefaultChunkLimit, 0)
	if err != nil {
		return currencyMigrationDraftHeaderResponse{}, err
	}
	return currencyMigrationDraftHeaderResponse{
		DraftID: row.DraftID, MigrationOperationID: row.MigrationOperationID, OperationKind: row.OperationKind,
		TargetCurrencyCode: row.TargetCurrencyCode, TargetCurrencySymbol: row.TargetCurrencySymbol,
		ExpectedInventoryID: positiveDecimalPtr(row.ExpectedInventoryID), ExpectedInventoryHash: row.ExpectedInventoryHash,
		ExpectedInventoryGeneration: row.ExpectedInventoryGeneration, ExpectedReportingCurrencyEpoch: row.ExpectedReportingCurrencyEpoch,
		ExpectedSettingsUpdatedAt: row.ExpectedSettingsUpdatedAt.UTC().Format(time.RFC3339), Status: row.Status,
		NormalizedHeaderHash: row.NormalizedHeaderHash, ReceivedChunkCount: row.ReceivedChunkCount, ChunkPage: page,
		DraftHash: row.DraftHash, TemplateCount: row.TemplateCount, CommittedResultOperationID: row.CommittedResultOperationID,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (s *Service) loadCurrencyDraftChunkPage(ctx context.Context, tx pgx.Tx, header currencyMigrationDraftHeaderRow, profileID, limit, lastOrdinal int) (currencyMigrationDraftChunkPage, error) {
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid`, header.DraftID).Scan(&total); err != nil {
		return currencyMigrationDraftChunkPage{}, err
	}
	rows, err := tx.Query(ctx, `SELECT ordinal, row_count, content_hash FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid AND ordinal > $2 ORDER BY ordinal ASC LIMIT $3`, header.DraftID, lastOrdinal, limit)
	if err != nil {
		return currencyMigrationDraftChunkPage{}, err
	}
	defer rows.Close()
	items := make([]currencyMigrationDraftChunkSummary, 0, limit)
	last := lastOrdinal
	for rows.Next() {
		var item currencyMigrationDraftChunkSummary
		if err := rows.Scan(&item.Ordinal, &item.RowCount, &item.ContentHash); err != nil {
			return currencyMigrationDraftChunkPage{}, err
		}
		items = append(items, item)
		last = item.Ordinal
	}
	if err := rows.Err(); err != nil {
		return currencyMigrationDraftChunkPage{}, err
	}
	page := currencyMigrationDraftChunkPage{Items: items, TotalCount: total, ConsumedCount: total}
	if len(items) > 0 {
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid AND ordinal > $2`, header.DraftID, last).Scan(&remaining); err != nil {
			return currencyMigrationDraftChunkPage{}, err
		}
		if remaining > 0 {
			page.ConsumedCount = total - remaining
			cursor := s.encodeCurrencyDraftCursor(currencyDraftCursor{Version: 1, ProfileID: profileID, DraftID: header.DraftID, Kind: "chunks", Binding: header.NormalizedHeaderHash, LastOrdinal: last})
			page.NextCursor = &cursor
		}
	} else if lastOrdinal > 0 {
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid AND ordinal > $2`, header.DraftID, lastOrdinal).Scan(&remaining); err != nil {
			return currencyMigrationDraftChunkPage{}, err
		}
		page.ConsumedCount = total - remaining
	}
	return page, nil
}

func (s *Service) loadCurrencyDraftItemPage(ctx context.Context, tx pgx.Tx, header currencyMigrationDraftHeaderRow, profileID, limit, lastID int) (currencyMigrationDraftItemPage, error) {
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_items WHERE draft_id = $1::uuid`, header.DraftID).Scan(&total); err != nil {
		return currencyMigrationDraftItemPage{}, err
	}
	rows, err := tx.Query(ctx, `SELECT template_id, template_name, expected_version, expected_updated_at, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, reference_count FROM pricing_currency_migration_draft_items WHERE draft_id = $1::uuid AND template_id > $2 ORDER BY template_id ASC LIMIT $3`, header.DraftID, lastID, limit)
	if err != nil {
		return currencyMigrationDraftItemPage{}, err
	}
	defer rows.Close()
	items := make([]currencyMigrationDraftItemResponse, 0, limit)
	last := lastID
	for rows.Next() {
		var item currencyMigrationDraftItemResponse
		var expected time.Time
		if err := rows.Scan(&item.TemplateID, &item.TemplateName, &item.ExpectedVersion, &expected, &item.InputPrice, &item.OutputPrice, &item.CachedInputPrice, &item.CacheCreationPrice, &item.ReasoningPrice, &item.ReferenceCount); err != nil {
			return currencyMigrationDraftItemPage{}, err
		}
		item.ExpectedUpdatedAt = expected.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
		last = item.TemplateID
	}
	if err := rows.Err(); err != nil {
		return currencyMigrationDraftItemPage{}, err
	}
	page := currencyMigrationDraftItemPage{Items: items, TotalCount: total, Limit: limit}
	if len(items) > 0 {
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_items WHERE draft_id = $1::uuid AND template_id > $2`, header.DraftID, last).Scan(&remaining); err != nil {
			return currencyMigrationDraftItemPage{}, err
		}
		page.HasMore = remaining > 0
	} else if lastID > 0 {
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_items WHERE draft_id = $1::uuid AND template_id > $2`, header.DraftID, lastID).Scan(&remaining); err != nil {
			return currencyMigrationDraftItemPage{}, err
		}
		page.HasMore = remaining > 0
	}
	if page.HasMore {
		cursor := s.encodeCurrencyDraftCursor(currencyDraftCursor{Version: 1, ProfileID: profileID, DraftID: header.DraftID, Kind: "items", Binding: stringValue(header.DraftHash), LastID: last})
		page.NextCursor = &cursor
	}
	return page, nil
}

func loadCurrencyDraftPayloadRows(ctx context.Context, tx pgx.Tx, draftID string) ([]currencyMigrationDraftItem, error) {
	rows, err := tx.Query(ctx, `SELECT ordinal, row_count, content_hash, payload FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid ORDER BY ordinal ASC`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all := make([]currencyMigrationDraftItem, 0)
	for rows.Next() {
		var ordinal, rowCount int
		var expectedHash string
		var payload []byte
		if err := rows.Scan(&ordinal, &rowCount, &expectedHash, &payload); err != nil {
			return nil, err
		}
		var items []currencyMigrationDraftItem
		if err := json.Unmarshal(payload, &items); err != nil {
			return nil, fmt.Errorf("decode currency migration chunk %d: %w", ordinal, err)
		}
		if len(items) != rowCount || currencyDraftItemsHash(items) != expectedHash {
			return nil, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_corrupt"}
		}
		all = append(all, items...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return all, nil
}

func loadCurrencyDraftItems(ctx context.Context, tx pgx.Tx, draftID string) ([]currencyMigrationDraftItem, error) {
	rows, err := tx.Query(ctx, `SELECT template_id, expected_version, expected_updated_at, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, template_name, reference_count FROM pricing_currency_migration_draft_items WHERE draft_id = $1::uuid ORDER BY template_id ASC`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]currencyMigrationDraftItem, 0)
	for rows.Next() {
		var item currencyMigrationDraftItem
		var expected time.Time
		if err := rows.Scan(&item.TemplateID, &item.ExpectedVersion, &expected, &item.InputPrice, &item.OutputPrice, &item.CachedInputPrice, &item.CacheCreationPrice, &item.ReasoningPrice, &item.TemplateName, &item.ReferenceCount); err != nil {
			return nil, err
		}
		item.ExpectedUpdatedAt = expected.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadCurrencyDraftAuthoritativeTemplates(ctx context.Context, tx pgx.Tx, profileID int, forUpdate bool) ([]currencyDraftAuthoritativeTemplate, error) {
	var currentEpochID *int64
	if err := tx.QueryRow(ctx, `SELECT current_reporting_currency_epoch_id FROM user_settings WHERE profile_id = $1`, profileID).Scan(&currentEpochID); err != nil {
		return nil, fmt.Errorf("load currency migration epoch pointer: %w", err)
	}
	var inventoryID int64
	if currentEpochID == nil {
		if err := tx.QueryRow(ctx, `SELECT inventory.inventory_id
			FROM pricing_migration_inventories AS inventory
			WHERE inventory.profile_id = $1
			  AND NOT EXISTS (SELECT 1 FROM pricing_migration_inventories AS successor WHERE successor.supersedes_inventory_id = inventory.inventory_id)
			ORDER BY inventory.generation DESC LIMIT 1`, profileID).Scan(&inventoryID); err != nil {
			if err == pgx.ErrNoRows {
				return nil, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_stale: pending pricing profile has no authoritative inventory"}
			}
			return nil, fmt.Errorf("load pending pricing inventory: %w", err)
		}
	}
	query := `SELECT templates.id, templates.name, templates.updated_at,
		revisions.id, revisions.version, revisions.input_price, revisions.output_price,
		revisions.cached_input_price, revisions.cache_creation_price, revisions.reasoning_price,
		evidence.legacy_template_evidence_id, evidence.public_version, evidence.input_price, evidence.output_price,
		evidence.cached_input_price, evidence.cache_creation_price, evidence.reasoning_price,
		(SELECT count(*) FROM connections AS c WHERE c.profile_id = templates.profile_id AND c.pricing_template_id = templates.id)
		FROM pricing_templates AS templates
		LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id
		LEFT JOIN pricing_migration_legacy_template_evidence AS evidence ON evidence.inventory_id = $2 AND evidence.template_id = templates.id
		WHERE templates.profile_id = $1 AND templates.deleted_at IS NULL ORDER BY templates.id ASC`
	if forUpdate {
		query += ` FOR UPDATE OF templates`
	}
	rows, err := tx.Query(ctx, query, profileID, inventoryID)
	if err != nil {
		return nil, fmt.Errorf("load active currency migration templates: %w", err)
	}
	defer rows.Close()
	items := make([]currencyDraftAuthoritativeTemplate, 0)
	for rows.Next() {
		var item currencyDraftAuthoritativeTemplate
		var revisionID, revisionVersion, evidenceID, evidenceVersion sql.NullInt64
		var revisionInput, revisionOutput, revisionCached, revisionCreation, revisionReasoning sql.NullString
		var evidenceInput, evidenceOutput, evidenceCached, evidenceCreation, evidenceReasoning sql.NullString
		if err := rows.Scan(&item.ID, &item.Name, &item.UpdatedAt, &revisionID, &revisionVersion, &revisionInput, &revisionOutput, &revisionCached, &revisionCreation, &revisionReasoning, &evidenceID, &evidenceVersion, &evidenceInput, &evidenceOutput, &evidenceCached, &evidenceCreation, &evidenceReasoning, &item.ReferenceCount); err != nil {
			return nil, err
		}
		if revisionID.Valid {
			value := revisionID.Int64
			item.RevisionID = &value
			item.Version = int(revisionVersion.Int64)
			item.InputPrice = nullableStringValue(revisionInput)
			item.OutputPrice = nullableStringValue(revisionOutput)
			item.CachedInputPrice = nullableStringValue(revisionCached)
			item.CacheCreationPrice = nullableStringValue(revisionCreation)
			item.ReasoningPrice = nullableStringValue(revisionReasoning)
		} else if evidenceID.Valid {
			value := evidenceID.Int64
			item.LegacyEvidenceID = &value
			if !evidenceVersion.Valid {
				return nil, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_corrupt: legacy template evidence has no public version"}
			}
			item.Version = int(evidenceVersion.Int64)
			item.InputPrice = nullableStringValue(evidenceInput)
			item.OutputPrice = nullableStringValue(evidenceOutput)
			item.CachedInputPrice = nullableStringValue(evidenceCached)
			item.CacheCreationPrice = nullableStringValue(evidenceCreation)
			item.ReasoningPrice = nullableStringValue(evidenceReasoning)
		} else {
			return nil, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_stale: active template has no current revision or matching legacy evidence"}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func sealCurrencyDraftItems(authoritative []currencyDraftAuthoritativeTemplate, rows []currencyMigrationDraftItem) ([]currencyMigrationDraftItem, error) {
	byID := make(map[int]currencyMigrationDraftItem, len(rows))
	for _, row := range rows {
		if _, exists := byID[row.TemplateID]; exists {
			return nil, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_duplicate_template: template_id=%d", row.TemplateID)}
		}
		byID[row.TemplateID] = row
	}
	items := make([]currencyMigrationDraftItem, 0, len(authoritative))
	missing := make([]string, 0)
	for _, template := range authoritative {
		row, ok := byID[template.ID]
		if !ok {
			missing = append(missing, strconv.Itoa(template.ID))
			continue
		}
		expected, err := time.Parse(time.RFC3339Nano, row.ExpectedUpdatedAt)
		if err != nil || row.ExpectedVersion != template.Version || !expected.UTC().Equal(template.UpdatedAt.UTC()) {
			return nil, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_stale: template_id=%d", template.ID)}
		}
		row.TemplateName, row.ReferenceCount = template.Name, template.ReferenceCount
		items = append(items, row)
		delete(byID, template.ID)
	}
	if len(missing) > 0 || len(byID) > 0 {
		extra := make([]string, 0, len(byID))
		for id := range byID {
			extra = append(extra, strconv.Itoa(id))
		}
		return nil, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_template_set_changed: missing=%s extra=%s", strings.Join(boundedStringSlice(missing), ","), strings.Join(boundedStringSlice(extra), ","))}
	}
	return items, nil
}

type computedCurrencyPreview struct {
	Response    currencyMigrationDraftPreviewResponse
	Items       []currencyMigrationPreviewItem
	PreviewHash string
}

func buildCurrencyMigrationPreview(ctx context.Context, tx pgx.Tx, header currencyMigrationDraftHeaderRow, profileID int) (computedCurrencyPreview, error) {
	settingsRow, found, err := loadUserSettings(ctx, tx, profileID, false)
	if err != nil || !found {
		if err != nil {
			return computedCurrencyPreview{}, err
		}
		return computedCurrencyPreview{}, fmt.Errorf("currency settings are missing")
	}
	return buildCurrencyMigrationPreviewWithSettings(ctx, tx, header, profileID, settingsRow, false)
}

func buildCurrencyMigrationPreviewWithSettings(ctx context.Context, tx pgx.Tx, header currencyMigrationDraftHeaderRow, profileID int, settingsRow userSettingsRow, forUpdate bool) (computedCurrencyPreview, error) {
	if header.Status != "sealed" && header.Status != "committed" {
		return computedCurrencyPreview{}, currencyMigrationDraftStateError(header.Status)
	}
	if header.DraftHash == nil {
		return computedCurrencyPreview{}, currencyMigrationDraftConflict()
	}
	currentEpoch, err := loadCurrencyMigrationEpochOnly(ctx, tx, settingsRow)
	if err != nil {
		return computedCurrencyPreview{}, err
	}
	authoritative, err := loadCurrencyDraftAuthoritativeTemplates(ctx, tx, profileID, forUpdate)
	if err != nil {
		return computedCurrencyPreview{}, err
	}
	draftItems, err := loadCurrencyDraftItems(ctx, tx, header.DraftID)
	if err != nil {
		return computedCurrencyPreview{}, err
	}
	byID := make(map[int]currencyMigrationDraftItem, len(draftItems))
	for _, item := range draftItems {
		byID[item.TemplateID] = item
	}
	previewItems := make([]currencyMigrationPreviewItem, 0, len(authoritative))
	for _, template := range authoritative {
		item, ok := byID[template.ID]
		if !ok {
			return computedCurrencyPreview{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_template_set_changed: template_id=%d", template.ID)}
		}
		expected, parseErr := time.Parse(time.RFC3339Nano, item.ExpectedUpdatedAt)
		if parseErr != nil || expected.UTC() != template.UpdatedAt.UTC() || item.ExpectedVersion != template.Version {
			return computedCurrencyPreview{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_stale: template_id=%d", template.ID)}
		}
		previewItems = append(previewItems, currencyMigrationPreviewItem{
			TemplateID: template.ID, Name: template.Name, CurrentVersion: template.Version, NextVersion: template.Version + 1,
			CurrentInputPrice: template.InputPrice, CurrentOutputPrice: template.OutputPrice, CurrentCachedInputPrice: template.CachedInputPrice,
			CurrentCacheCreationPrice: template.CacheCreationPrice, CurrentReasoningPrice: template.ReasoningPrice,
			NewInputPrice: item.InputPrice, NewOutputPrice: item.OutputPrice, NewCachedInputPrice: item.CachedInputPrice,
			NewCacheCreationPrice: item.CacheCreationPrice, NewReasoningPrice: item.ReasoningPrice, ReferenceCount: template.ReferenceCount,
		})
		delete(byID, template.ID)
	}
	if len(byID) != 0 || len(previewItems) != len(draftItems) {
		return computedCurrencyPreview{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_template_set_changed"}
	}
	previewHash := hashCurrencyMigrationPreview(header, settingsRow, currentEpoch, previewItems)
	nextEpoch := 1
	if currentEpoch != nil {
		nextEpoch = *currentEpoch + 1
	}
	nextEpochValue := nextEpoch
	response := currencyMigrationDraftPreviewResponse{
		OperationKind: header.OperationKind, MigrationOperationID: header.MigrationOperationID, DraftID: header.DraftID, DraftHash: stringValue(header.DraftHash), PreviewHash: previewHash,
		TargetCurrencyCode: header.TargetCurrencyCode, TargetCurrencySymbol: header.TargetCurrencySymbol, CurrentCurrencyCode: nullableNonEmptyString(settingsRow.ReportCurrencyCode),
		CurrentEpoch: currentEpoch, NextEpoch: &nextEpochValue, TemplateCount: len(previewItems), RevisionChangeCount: len(previewItems), Committable: true, ValidationErrors: []map[string]any{}, EpochChange: true,
	}
	return computedCurrencyPreview{Response: response, Items: previewItems, PreviewHash: previewHash}, nil
}

func hashCurrencyMigrationPreview(header currencyMigrationDraftHeaderRow, settingsRow userSettingsRow, currentEpoch *int, items []currencyMigrationPreviewItem) string {
	canonical := struct {
		HeaderHash string                         `json:"header_hash"`
		DraftHash  string                         `json:"draft_hash"`
		SettingsAt string                         `json:"settings_updated_at"`
		Code       string                         `json:"code"`
		Epoch      *int                           `json:"epoch"`
		Items      []currencyMigrationPreviewItem `json:"items"`
	}{header.NormalizedHeaderHash, stringValue(header.DraftHash), settingsRow.UpdatedAt.UTC().Format(time.RFC3339Nano), settingsRow.ReportCurrencyCode, currentEpoch, items}
	raw, _ := json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func applyCurrencyMigrationDraftCutover(ctx context.Context, tx pgx.Tx, profileID int, settingsRow userSettingsRow, header currencyMigrationDraftHeaderRow, operationID, previewHash string, templates []currencyDraftAuthoritativeTemplate, items []currencyMigrationDraftItem, currentTime time.Time) (currencyMigrationCommitResponse, error) {
	currentEpoch, err := loadCurrencyMigrationEpochOnly(ctx, tx, settingsRow)
	if err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	nextEpoch := 1
	if currentEpoch != nil {
		nextEpoch = *currentEpoch + 1
	}
	var inventoryID *int64
	var reportingEvidenceID *int64
	var inventoryGeneration, inventorySettingsGeneration, inventoryTemplateGeneration, inventoryReferenceGeneration int64
	var inventoryFXCount, inventoryFXDependencyCount int
	if header.ExpectedInventoryID != nil {
		parsedInventoryID := *header.ExpectedInventoryID
		if parsedInventoryID < 1 {
			return currencyMigrationCommitResponse{}, currencyMigrationInventoryStale()
		}
		inventoryID = &parsedInventoryID
		if err := tx.QueryRow(ctx, `SELECT generation, settings_generation, template_generation, reference_generation, fx_evidence_count, fx_dependency_count
			FROM pricing_migration_inventories
			WHERE inventory_id = $1 AND profile_id = $2
			  AND NOT EXISTS (SELECT 1 FROM pricing_migration_inventories AS successor WHERE successor.supersedes_inventory_id = pricing_migration_inventories.inventory_id)
			FOR SHARE`, parsedInventoryID, profileID).Scan(&inventoryGeneration, &inventorySettingsGeneration, &inventoryTemplateGeneration, &inventoryReferenceGeneration, &inventoryFXCount, &inventoryFXDependencyCount); err != nil {
			return currencyMigrationCommitResponse{}, currencyMigrationInventoryStale()
		}
		if inventoryFXCount != 0 || inventoryFXDependencyCount != 0 {
			return currencyMigrationCommitResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_conflict: live or unclassified FX evidence must be resolved before pre-epoch currency cutover"}
		}
		if err := tx.QueryRow(ctx, `SELECT legacy_reporting_currency_evidence_id FROM pricing_migration_legacy_reporting_currency_evidence WHERE inventory_id = $1`, parsedInventoryID).Scan(&reportingEvidenceID); err != nil && err != pgx.ErrNoRows {
			return currencyMigrationCommitResponse{}, err
		}
	}
	var oldEpochID *int64
	if settingsRow.CurrentReportingCurrencyEpochID != nil {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM reporting_currency_epochs WHERE id = $1 FOR UPDATE`, *settingsRow.CurrentReportingCurrencyEpochID).Scan(&id); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		oldEpochID = &id
	}
	cutoverAt := currentTime
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&cutoverAt); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	if oldEpochID != nil {
		if _, err := tx.Exec(ctx, `UPDATE reporting_currency_epochs SET superseded_at = $2, updated_at = $2 WHERE id = $1`, *oldEpochID, cutoverAt); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
	}
	var newEpochID int64
	if err := tx.QueryRow(ctx, `INSERT INTO reporting_currency_epochs (profile_id, epoch, currency_code, currency_symbol, effective_at, superseded_at, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, NULL, $5, $5) RETURNING id`, profileID, nextEpoch, header.TargetCurrencyCode, header.TargetCurrencySymbol, cutoverAt).Scan(&newEpochID); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	// The operation parent stores the sealed draft hash as its normalized
	// payload identity; preview_hash is a separate CAS dimension. This lets a
	// lost-response retry validate the strict commit request before taking the
	// profile lock and makes same-operation/different-draft conflicts explicit.
	payloadHash := stringValue(header.DraftHash)
	response := currencyMigrationCommitResponse{OldCurrencyCode: nullableNonEmptyString(settingsRow.ReportCurrencyCode), NewCurrencyCode: header.TargetCurrencyCode, OldEpoch: currentEpoch, NewEpoch: intPtr(nextEpoch), RevisionChangeCount: len(items), TemplateCount: len(items), MigrationOperationID: operationID, EpochChange: true}
	resultRaw, _ := json.Marshal(response)
	resultHash := sha256.Sum256(resultRaw)
	if _, err := tx.Exec(ctx, `INSERT INTO pricing_mutation_operations (operation_id, profile_id, result_kind, normalized_payload_hash, preview_hash, operation_recorded_at, success_summary, result_hash, created_at) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::jsonb, $8, $6)`, operationID, profileID, header.OperationKind, payloadHash, previewHash, cutoverAt, resultRaw, hex.EncodeToString(resultHash[:])); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	byID := make(map[int]currencyMigrationDraftItem, len(items))
	for _, item := range items {
		byID[item.TemplateID] = item
	}
	ledgerItemsHashInput := make([]map[string]any, 0, len(templates))
	for index, template := range templates {
		item, ok := byID[template.ID]
		if !ok {
			return currencyMigrationCommitResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_template_set_changed"}
		}
		var revisionID int64
		if err := tx.QueryRow(ctx, `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, effective_at, created_at, created_by_kind, created_by_operation_id) VALUES ($1, $2, 'PER_1M', $3, $4, $5, 'active_epoch', $6, $7, $8, $9, $10, $11, $11, 'currency_migration', $12::uuid) RETURNING id`, template.ID, template.Version+1, header.TargetCurrencyCode, newEpochID, nextEpoch, item.InputPrice, item.OutputPrice, item.CachedInputPrice, item.CacheCreationPrice, item.ReasoningPrice, cutoverAt, operationID).Scan(&revisionID); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3 AND profile_id = $4 AND deleted_at IS NULL`, revisionID, cutoverAt, template.ID, profileID); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		if err := insertPricingMutationResultItemSettings(ctx, tx, operationID, index+1, template.ID, template.Version+1, revisionID, cutoverAt, template.Name); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO currency_migration_ledger_items (operation_id, ordinal, template_id, template_name_snapshot, old_version, new_version, old_revision_id, old_template_evidence_id, new_revision_id, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`, operationID, index+1, template.ID, template.Name, template.Version, template.Version+1, template.RevisionID, template.LegacyEvidenceID, revisionID, item.InputPrice, item.OutputPrice, item.CachedInputPrice, item.CacheCreationPrice, item.ReasoningPrice); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		ledgerItemsHashInput = append(ledgerItemsHashInput, map[string]any{"template_id": template.ID, "old_version": template.Version, "new_version": template.Version + 1, "revision_id": revisionID})
	}
	itemsRaw, _ := json.Marshal(ledgerItemsHashInput)
	itemsHash := sha256.Sum256(itemsRaw)
	if _, err := tx.Exec(ctx, `INSERT INTO currency_migration_ledger (operation_id, operation_kind, profile_id, old_epoch_id, old_epoch, new_epoch_id, new_epoch, legacy_reporting_currency_evidence_id, normalized_payload_hash, inventory_id, inventory_hash, item_count, items_hash, committed_result, committed_at) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15)`, operationID, header.OperationKind, profileID, oldEpochID, currentEpoch, newEpochID, nextEpoch, reportingEvidenceID, payloadHash, inventoryID, inventoryHashForLedger(header), len(templates), hex.EncodeToString(itemsHash[:]), resultRaw, cutoverAt); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE user_settings SET report_currency_code = $2, report_currency_symbol = $3, current_reporting_currency_epoch_id = $4, pricing_migration_state = 'ready', legacy_migration_issues = '{}', pricing_template_generation = pricing_template_generation + 1, updated_at = $5 WHERE id = $1`, settingsRow.ID, header.TargetCurrencyCode, header.TargetCurrencySymbol, newEpochID, cutoverAt); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	if inventoryID != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO pricing_migration_inventories (
			profile_id, generation, supersedes_inventory_id, settings_generation, epoch_generation,
			template_generation, reference_generation, issue_codes, fx_evidence_count,
			fx_assessment_count, fx_dependency_count, template_evidence_count,
			reporting_currency_evidence_count, fx_evidence_hash_root, template_evidence_hash_root,
			reporting_currency_evidence_hash_root, legacy_fx_source_count, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, '{}', 0, 0, 0, 0, 0, NULL, NULL, NULL, 0, $8)`,
			profileID, inventoryGeneration+1, *inventoryID, inventorySettingsGeneration+1, int64(nextEpoch), inventoryTemplateGeneration+1, inventoryReferenceGeneration, cutoverAt); err != nil {
			return currencyMigrationCommitResponse{}, fmt.Errorf("append clean currency migration inventory: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE pricing_currency_migration_drafts SET status = 'committed', committed_result_operation_id = $2::uuid, updated_at = $3 WHERE draft_id = $1::uuid`, header.DraftID, operationID, cutoverAt); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	return response, nil
}

func insertPricingMutationResultItemSettings(ctx context.Context, tx pgx.Tx, operationID string, ordinal, templateID, version int, revisionID int64, effectiveAt time.Time, name string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pricing_mutation_result_items (operation_id, ordinal, template_id, action, version, revision_id, revision_effective_at, template_name_snapshot) VALUES ($1::uuid, $2, $3, 'revision_created', $4, $5, $6, $7)`, operationID, ordinal, templateID, version, revisionID, effectiveAt, name)
	return err
}

func loadCurrencyMigrationResult(ctx context.Context, tx pgx.Tx, operationID string) (currencyMigrationCommitResponse, bool, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT committed_result FROM currency_migration_ledger WHERE operation_id = $1::uuid`, operationID).Scan(&raw)
	if err == pgx.ErrNoRows {
		return currencyMigrationCommitResponse{}, false, nil
	}
	if err != nil {
		return currencyMigrationCommitResponse{}, false, err
	}
	var response currencyMigrationCommitResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return currencyMigrationCommitResponse{}, false, err
	}
	return response, true, nil
}

func currencyMigrationCommitPayloadHash(header currencyMigrationDraftHeaderRow, previewHash string) string {
	sum := sha256.Sum256([]byte(header.NormalizedHeaderHash + ":" + stringValue(header.DraftHash) + ":" + previewHash))
	return hex.EncodeToString(sum[:])
}

func hashCanonicalCurrencyDraft(headerHash string, items []currencyMigrationDraftItem) string {
	canonical := append([]currencyMigrationDraftItem(nil), items...)
	// Items are already ordered by the authoritative template query, but the
	// explicit sort makes the hash independent of chunk order.
	sortCurrencyDraftItems(canonical)
	raw, _ := json.Marshal(struct {
		HeaderHash string                       `json:"header_hash"`
		Items      []currencyMigrationDraftItem `json:"items"`
	}{headerHash, canonical})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func currencyDraftItemsHash(items []currencyMigrationDraftItem) string {
	canonical := append([]currencyMigrationDraftItem(nil), items...)
	sortCurrencyDraftItems(canonical)
	raw, _ := json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeCurrencyDraftChunk(rows []currencyMigrationDraftChunkRowRequest) ([]currencyMigrationDraftItem, string, error) {
	if len(rows) < 1 || len(rows) > currencyDraftChunkMaxRows {
		return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "currency migration chunk must contain 1-100 items"}
	}
	items := make([]currencyMigrationDraftItem, 0, len(rows))
	seen := make(map[int]struct{}, len(rows))
	for _, row := range rows {
		if row.TemplateID < 1 || row.ExpectedVersion < 1 {
			return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "template_id and expected_version must be positive"}
		}
		if _, ok := seen[row.TemplateID]; ok {
			return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("duplicate template_id %d in chunk", row.TemplateID)}
		}
		seen[row.TemplateID] = struct{}{}
		expected, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(row.ExpectedUpdatedAt))
		if err != nil {
			return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "expected_updated_at must be a valid RFC3339 timestamp"}
		}
		input, err := canonicalCurrencyMigrationPrice("input_price", row.InputPrice, false)
		if err != nil {
			return nil, "", err
		}
		output, err := canonicalCurrencyMigrationPrice("output_price", row.OutputPrice, false)
		if err != nil {
			return nil, "", err
		}
		cached, err := canonicalCurrencyMigrationPrice("cached_input_price", row.CachedInputPrice, true)
		if err != nil {
			return nil, "", err
		}
		cacheCreation, err := canonicalCurrencyMigrationPrice("cache_creation_price", row.CacheCreationPrice, true)
		if err != nil {
			return nil, "", err
		}
		reasoning, err := canonicalCurrencyMigrationPrice("reasoning_price", row.ReasoningPrice, true)
		if err != nil {
			return nil, "", err
		}
		items = append(items, currencyMigrationDraftItem{TemplateID: row.TemplateID, ExpectedVersion: row.ExpectedVersion, ExpectedUpdatedAt: expected.UTC().Format(time.RFC3339Nano), InputPrice: input, OutputPrice: output, CachedInputPrice: nullableCurrencyPricePtr(cached), CacheCreationPrice: nullableCurrencyPricePtr(cacheCreation), ReasoningPrice: nullableCurrencyPricePtr(reasoning)})
	}
	sortCurrencyDraftItems(items)
	return items, currencyDraftItemsHash(items), nil
}

func nullableCurrencyPricePtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func canonicalCurrencyMigrationPrice(field string, raw *string, nullable bool) (string, error) {
	if raw == nil {
		if nullable {
			return "", nil
		}
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " is required and must not be null"}
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" || len(trimmed) > 20 {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " must be a canonical non-negative decimal string"}
	}
	integral, fractional := trimmed, ""
	for i, ch := range trimmed {
		if ch == '.' {
			integral, fractional = trimmed[:i], trimmed[i+1:]
			break
		}
		if ch < '0' || ch > '9' {
			return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " must be a canonical non-negative decimal string"}
		}
	}
	if integral == "" || strings.ContainsAny(fractional, "+-eE") {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " must be a canonical non-negative decimal string"}
	}
	for _, ch := range fractional {
		if ch < '0' || ch > '9' {
			return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " must be a canonical non-negative decimal string"}
		}
	}
	integral = strings.TrimLeft(integral, "0")
	if integral == "" {
		integral = "0"
	}
	fractional = strings.TrimRight(fractional, "0")
	canonical := integral
	if fractional != "" {
		canonical += "." + fractional
	}
	if len(canonical) > 20 {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " must be a canonical non-negative decimal string"}
	}
	return canonical, nil
}

func sortCurrencyDraftItems(items []currencyMigrationDraftItem) {
	slicesSort(items, func(left, right currencyMigrationDraftItem) bool { return left.TemplateID < right.TemplateID })
}

func slicesSort[T any](items []T, less func(T, T) bool) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && less(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

type currencyMigrationPreviewPageWithLastID struct {
	page   currencyMigrationPreviewPage
	lastID int
}

func loadCurrencyMigrationPreviewPage(items []currencyMigrationPreviewItem, limit, lastID int) (currencyMigrationPreviewPageWithLastID, error) {
	if limit < 1 {
		return currencyMigrationPreviewPageWithLastID{}, fmt.Errorf("preview page limit must be positive")
	}
	start := 0
	for start < len(items) && items[start].TemplateID <= lastID {
		start++
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	pageItems := append([]currencyMigrationPreviewItem(nil), items[start:end]...)
	page := currencyMigrationPreviewPage{Items: pageItems, TotalCount: len(items), Limit: limit, HasMore: end < len(items)}
	if page.HasMore && len(pageItems) > 0 {
		placeholder := "pending"
		page.NextCursor = &placeholder
	}
	last := lastID
	if len(pageItems) > 0 {
		last = pageItems[len(pageItems)-1].TemplateID
	}
	return currencyMigrationPreviewPageWithLastID{page: page, lastID: last}, nil
}

func firstCurrencyMigrationPreviewPage(items []currencyMigrationPreviewItem, limit, profileID int, draftID, binding string, s *Service) currencyMigrationPreviewPage {
	page, _ := loadCurrencyMigrationPreviewPage(items, limit, 0)
	if page.page.HasMore {
		cursor := s.encodeCurrencyDraftCursor(currencyDraftCursor{Version: 1, ProfileID: profileID, DraftID: draftID, Kind: "preview-items", Binding: binding, LastID: page.lastID})
		page.page.NextCursor = &cursor
	}
	return page.page
}

func currencyDraftPageLimit(r *http.Request, defaultLimit, maxLimit int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxLimit {
		return 0, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("limit must be between 1 and %d", maxLimit)}
	}
	return limit, nil
}

func (s *Service) encodeCurrencyDraftCursor(cursor currencyDraftCursor) string {
	cursor.Version = 1
	payload, _ := json.Marshal(cursor)
	mac := hmac.New(sha256.New, s.currencyCursorKey)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
}

func (s *Service) decodeCurrencyDraftCursor(raw string, expected currencyDraftCursor) (currencyDraftCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return currencyDraftCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) <= sha256.Size {
		return currencyDraftCursor{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "currency migration cursor is invalid"}
	}
	payload, signature := decoded[:len(decoded)-sha256.Size], decoded[len(decoded)-sha256.Size:]
	mac := hmac.New(sha256.New, s.currencyCursorKey)
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return currencyDraftCursor{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "currency migration cursor is invalid"}
	}
	var cursor currencyDraftCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Version != 1 || cursor.ProfileID != expected.ProfileID || cursor.DraftID != expected.DraftID || cursor.Kind != expected.Kind || cursor.Binding != expected.Binding {
		return currencyDraftCursor{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "currency migration cursor is stale"}
	}
	return cursor, nil
}

func normalizeUUIDV4(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", fmt.Errorf("invalid uuid")
	}
	compact := strings.ReplaceAll(value, "-", "")
	decoded, err := hex.DecodeString(compact)
	if err != nil || len(decoded) != 16 || decoded[6]>>4 != 4 || decoded[8]&0xc0 != 0x80 {
		return "", fmt.Errorf("invalid uuidv4")
	}
	return value, nil
}

func canonicalPositiveDecimal(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 || strconv.FormatInt(parsed, 10) != value {
		return "", fmt.Errorf("invalid positive decimal")
	}
	return strconv.FormatInt(parsed, 10), nil
}

func positiveDecimalPtr(value *int64) *string {
	if value == nil {
		return nil
	}
	result := strconv.FormatInt(*value, 10)
	return &result
}

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

func inventoryHashForLedger(header currencyMigrationDraftHeaderRow) string {
	if header.ExpectedInventoryHash != nil {
		return *header.ExpectedInventoryHash
	}
	return ""
}

func boundedStringSlice(values []string) []string {
	if len(values) > 20 {
		return append(values[:20], fmt.Sprintf("+%d more", len(values)-20))
	}
	return values
}

func currencyMigrationOperationConflict() error {
	return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_operation_conflict: operation identity is already bound to a different draft or payload"}
}

func currencyMigrationDraftConflict() error {
	return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_conflict: draft identity or content does not match"}
}

func currencyMigrationDraftStateError(state string) error {
	return &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_state_%s", state)}
}

func currencyMigrationPreviewStale() error {
	return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_stale: preview no longer matches the sealed draft or current settings"}
}

func loadCurrencyMigrationEpochOnly(ctx context.Context, tx pgx.Tx, settingsRow userSettingsRow) (*int, error) {
	if settingsRow.CurrentReportingCurrencyEpochID == nil {
		return nil, nil
	}
	var epoch int
	if err := tx.QueryRow(ctx, `SELECT epoch FROM reporting_currency_epochs WHERE id = $1`, *settingsRow.CurrentReportingCurrencyEpochID).Scan(&epoch); err != nil {
		return nil, fmt.Errorf("load current reporting currency epoch: %w", err)
	}
	return &epoch, nil
}

func canonicalCurrencyCode(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) != 3 {
		return ""
	}
	upper := strings.ToUpper(trimmed)
	for _, char := range upper {
		if char < 'A' || char > 'Z' {
			return ""
		}
	}
	return upper
}
