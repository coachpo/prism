package settings

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/jackc/pgx/v5"
)

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
		return applyCurrencyMigrationDraftCutover(r.Context(), tx, profile.ID, settingsRow, header, operationID, computed.PreviewHash, computed.AuthoritativeTemplates, computed.DraftItems, s.nowUTC())
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
