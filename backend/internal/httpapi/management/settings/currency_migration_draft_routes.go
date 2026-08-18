package settings

import (
	"context"
	"crypto/sha256"
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

const currencyDraftTTL = 24 * time.Hour

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
		if err := rejectCurrencyMigrationWithTieredTemplates(r.Context(), tx, profile.ID); err != nil {
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
