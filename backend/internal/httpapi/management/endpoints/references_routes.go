package endpoints

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// referencePageDefaults is the bounded disclosure/delete page policy.
const (
	referencePageDefaultLimit = 50
	referencePageMaxLimit     = 100
)

// handleEndpointReferencesBatch returns per-Endpoint direct-reference summaries
// for up to 100 IDs, preserving input order. Missing or cross-profile IDs are a
// typed 404; any SQL/map failure fails the whole batch (never a partial zero).
func (s *Service) handleEndpointReferencesBatch(w http.ResponseWriter, r *http.Request) {
	var requestBody endpointReferenceBatchRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(requestBody.EndpointIDs) < 1 || len(requestBody.EndpointIDs) > 100 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, map[string]any{"code": "validation_failed", "message": "endpoint_ids must contain 1..100 unique positive integers"})
		return
	}
	seen := map[int]struct{}{}
	for _, id := range requestBody.EndpointIDs {
		if id <= 0 {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, map[string]any{"code": "validation_failed", "message": "endpoint_ids must contain unique positive integers"})
			return
		}
		if _, exists := seen[id]; exists {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, map[string]any{"code": "validation_failed", "message": "endpoint_ids must not contain duplicates"})
			return
		}
		seen[id] = struct{}{}
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint-reference-batch", func(tx pgx.Tx) (endpointReferenceBatchResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return endpointReferenceBatchResponse{}, err
		}
		existing, err := listEndpointIDs(r.Context(), tx, profile.ID, requestBody.EndpointIDs)
		if err != nil {
			return endpointReferenceBatchResponse{}, err
		}
		missing := make([]int, 0)
		for _, id := range requestBody.EndpointIDs {
			if _, exists := existing[id]; !exists {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			return endpointReferenceBatchResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: map[string]any{"code": "endpoint_not_found", "message": "One or more Endpoints do not exist in this profile", "missing_endpoint_ids": missing}}
		}
		sets, err := loadCanonicalReferenceSets(r.Context(), tx, profile.ID, requestBody.EndpointIDs)
		if err != nil {
			return endpointReferenceBatchResponse{}, err
		}
		items := make([]endpointReferenceBatchItem, 0, len(requestBody.EndpointIDs))
		for _, id := range requestBody.EndpointIDs {
			set := sets[id]
			items = append(items, endpointReferenceBatchItem{EndpointID: id, Summary: set.Summary})
		}
		return endpointReferenceBatchResponse{Items: items}, nil
	})
	if err != nil {
		s.writeReferenceError(w, r, err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// handleEndpointReferencesDetail returns the bounded first page (or a
// continuation page along the same opaque snapshot cursor) for one Endpoint.
func (s *Service) handleEndpointReferencesDetail(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	rawLimit := strings.TrimSpace(r.URL.Query().Get("limit"))
	rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	limit := referencePageDefaultLimit
	if rawLimit != "" {
		parsed, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || parsed < 1 || parsed > referencePageMaxLimit {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, map[string]any{"code": "validation_failed", "fields": map[string]string{"limit": "limit_invalid"}})
			return
		}
		limit = parsed
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint-reference-detail", func(tx pgx.Tx) (endpointReferenceDetailResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return endpointReferenceDetailResponse{}, err
		}
		_, found, err := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, false)
		if err != nil {
			return endpointReferenceDetailResponse{}, err
		}
		if !found {
			return endpointReferenceDetailResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		sets, err := loadCanonicalReferenceSets(r.Context(), tx, profile.ID, []int{endpointID})
		if err != nil {
			return endpointReferenceDetailResponse{}, err
		}
		set := sets[endpointID]

		effectiveLimit := limit
		startIndex := 0
		if rawCursor != "" {
			cursor, decodeErr := decodeReferenceCursor(rawCursor, s.secretEncryptionKey)
			if decodeErr != nil {
				return endpointReferenceDetailResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: map[string]any{"code": "reference_cursor_invalid", "message": "The reference cursor is invalid or expired"}}
			}
			if cursor.ProfileID != profile.ID || cursor.EndpointID != endpointID {
				return endpointReferenceDetailResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: map[string]any{"code": "reference_cursor_mismatch", "message": "The reference cursor does not match this Endpoint"}}
			}
			if rawLimit != "" && cursor.Limit != limit {
				return endpointReferenceDetailResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: map[string]any{"code": "reference_cursor_mismatch", "message": "The reference cursor limit differs from the request"}}
			}
			if cursor.SnapshotHash != set.Hash {
				return endpointReferenceDetailResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: map[string]any{"code": "reference_snapshot_stale", "message": "The direct references changed; restart pagination from page one"}}
			}
			effectiveLimit = cursor.Limit
			startIndex = len(set.Items)
			for index, key := range set.OrderKeys {
				if key.encode() == cursor.LastKey {
					startIndex = index + 1
					break
				}
			}
			if startIndex > len(set.Items) {
				startIndex = len(set.Items)
			}
		}

		endIndex := startIndex + effectiveLimit
		if endIndex > len(set.Items) {
			endIndex = len(set.Items)
		}
		pageItems := set.Items[startIndex:endIndex]
		var nextCursor *string
		if endIndex < len(set.Items) {
			lastKey := set.OrderKeys[endIndex-1].encode()
			encoded, encodeErr := encodeReferenceCursor(referenceCursor{
				Version:      1,
				ProfileID:    profile.ID,
				EndpointID:   endpointID,
				Limit:        effectiveLimit,
				SnapshotHash: set.Hash,
				LastKey:      lastKey,
			}, s.secretEncryptionKey)
			if encodeErr != nil {
				return endpointReferenceDetailResponse{}, encodeErr
			}
			nextCursor = &encoded
		}
		return endpointReferenceDetailResponse{
			EndpointID: endpointID,
			Summary:    set.Summary,
			ReferencePage: endpointReferencePage{
				Items:                 pageItems,
				TotalCount:            len(set.Items),
				NextCursor:            nextCursor,
				ReferenceSnapshotHash: set.Hash,
			},
		}, nil
	})
	if err != nil {
		s.writeReferenceError(w, r, err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// handleDeleteEndpoint is the authoritative lock-time delete. It recomputes
// the canonical reference set under the write transaction; zero direct
// references allow deletion, anything else returns the typed 409 blocker.
func (s *Service) handleDeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint", func(tx pgx.Tx) (endpointDeletedResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return endpointDeletedResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return endpointDeletedResponse{}, err
		}
		record, found, err := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, true)
		if err != nil {
			return endpointDeletedResponse{}, err
		}
		if !found {
			return endpointDeletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		sets, err := loadCanonicalReferenceSets(r.Context(), tx, profile.ID, []int{endpointID})
		if err != nil {
			return endpointDeletedResponse{}, err
		}
		set := sets[endpointID]
		if set.Summary.DirectReferenceCount > 0 {
			page := firstReferencePage(set, referencePageDefaultLimit, profile.ID, endpointID, s.secretEncryptionKey)
			detail := endpointInUseDetail{
				Code:          "endpoint_in_use",
				Message:       "Endpoint is referenced by Terminal Targets",
				EndpointID:    endpointID,
				Summary:       set.Summary,
				ReferencePage: page,
				ReferencesURL: fmt.Sprintf("/api/endpoints/%d/references", endpointID),
			}
			return endpointDeletedResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: detail}
		}
		if err := deleteEndpoint(r.Context(), tx, endpointID); err != nil {
			return endpointDeletedResponse{}, err
		}
		_ = record
		return endpointDeletedResponse{Deleted: true}, nil
	})
	if err != nil {
		s.writeReferenceError(w, r, err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// handleOrphanConnectionCleanup deletes a single orphan Connection (no owner
// Access Target) under the profile/Endpoint/Connection locks. It never accepts
// owned Terminal Targets and never cascades an Endpoint.
func (s *Service) handleOrphanConnectionCleanup(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint-orphan-cleanup", func(tx pgx.Tx) (orphanCleanupResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return orphanCleanupResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return orphanCleanupResponse{}, err
		}
		_, endpointFound, err := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, true)
		if err != nil {
			return orphanCleanupResponse{}, err
		}
		if !endpointFound {
			return orphanCleanupResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		connection, found, err := loadConnectionForCleanup(r.Context(), tx, profile.ID, endpointID, connectionID)
		if err != nil {
			return orphanCleanupResponse{}, err
		}
		if !found {
			return orphanCleanupResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		owner, ownerFound, err := loadConnectionOwner(r.Context(), tx, profile.ID, connectionID)
		if err != nil {
			return orphanCleanupResponse{}, err
		}
		if ownerFound {
			item := endpointReferenceItem{
				Kind:               referenceKindOwned,
				ConnectionID:       connection.ID,
				TerminalTargetID:   connection.ID,
				TerminalTargetName: connection.Name,
				APIFamily:          connection.APIFamily,
				ConnectionIsActive: connection.IsActive,
				AccessTarget:       &endpointReferenceAccessTarget{ID: owner.MatID, Position: owner.MatPosition, IsEnabled: owner.MatEnabled},
			}
			return orphanCleanupResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: connectionNotOrphanedDetail{Code: "connection_not_orphaned", Message: "This Terminal Target now has an owner model; manage it from the model detail page", Item: item}}
		}
		if err := deleteOrphanConnection(r.Context(), tx, connectionID); err != nil {
			return orphanCleanupResponse{}, err
		}
		return orphanCleanupResponse{Deleted: true, ConnectionID: connectionID}, nil
	})
	if err != nil {
		s.writeReferenceError(w, r, err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// firstReferencePage builds the bounded first page shared by DELETE 409 and
// single detail (identical row order and snapshot hash).
func firstReferencePage(set canonicalReferenceSet, limit int, profileID int, endpointID int, secretEncryptionKey string) endpointReferencePage {
	endIndex := limit
	if endIndex > len(set.Items) {
		endIndex = len(set.Items)
	}
	pageItems := set.Items[:endIndex]
	var nextCursor *string
	if endIndex < len(set.Items) {
		encoded, err := encodeReferenceCursor(referenceCursor{
			Version:      1,
			ProfileID:    profileID,
			EndpointID:   endpointID,
			Limit:        limit,
			SnapshotHash: set.Hash,
			LastKey:      set.OrderKeys[endIndex-1].encode(),
		}, secretEncryptionKey)
		if err == nil {
			nextCursor = &encoded
		}
	}
	return endpointReferencePage{
		Items:                 pageItems,
		TotalCount:            len(set.Items),
		NextCursor:            nextCursor,
		ReferenceSnapshotHash: set.Hash,
	}
}

func (s *Service) writeReferenceError(w http.ResponseWriter, r *http.Request, err error) {
	if integrityErr, ok := err.(*referenceIntegrityError); ok {
		writeDomainError(w, r, s.corsSnapshot(), referenceIntegrityErrorResponse(integrityErr.EndpointIDs, integrityErr.ConnectionIDs))
		return
	}
	writeDomainError(w, r, s.corsSnapshot(), err)
}

func listEndpointIDs(ctx context.Context, exec queryExecutor, profileID int, endpointIDs []int) (map[int]struct{}, error) {
	rows, err := exec.Query(ctx, `SELECT id FROM endpoints WHERE profile_id = $1 AND id = ANY($2)`, profileID, int32ArrayArg(endpointIDs))
	if err != nil {
		return nil, fmt.Errorf("query endpoint ids for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	found := map[int]struct{}{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan endpoint id: %w", err)
		}
		found[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoint ids for profile %d: %w", profileID, err)
	}
	return found, nil
}

type orphanConnectionRecord struct {
	ID        int
	APIFamily string
	IsActive  bool
	Name      *string
}

type orphanOwnerRecord struct {
	MatID      int
	MatPosition int
	MatEnabled bool
}

func loadConnectionForCleanup(ctx context.Context, exec queryExecutor, profileID int, endpointID int, connectionID int) (orphanConnectionRecord, bool, error) {
	var record orphanConnectionRecord
	var name sql.NullString
	if err := exec.QueryRow(ctx, `SELECT id, api_family, is_active, name FROM connections WHERE profile_id = $1 AND id = $2 AND endpoint_id = $3 LIMIT 1`, profileID, connectionID, endpointID).Scan(&record.ID, &record.APIFamily, &record.IsActive, &name); err == pgx.ErrNoRows {
		return orphanConnectionRecord{}, false, nil
	} else if err != nil {
		return orphanConnectionRecord{}, false, fmt.Errorf("load connection %d for orphan cleanup: %w", connectionID, err)
	}
	record.Name = nullableStringValue(name)
	return record, true, nil
}

func loadConnectionOwner(ctx context.Context, exec queryExecutor, profileID int, connectionID int) (orphanOwnerRecord, bool, error) {
	var record orphanOwnerRecord
	err := exec.QueryRow(ctx, `SELECT id, position, is_enabled FROM model_access_targets WHERE profile_id = $1 AND target_type = 'connection' AND target_connection_id = $2 LIMIT 1`, profileID, connectionID).Scan(&record.MatID, &record.MatPosition, &record.MatEnabled)
	if err == pgx.ErrNoRows {
		return orphanOwnerRecord{}, false, nil
	}
	if err != nil {
		return orphanOwnerRecord{}, false, fmt.Errorf("load owner for connection %d: %w", connectionID, err)
	}
	return record, true, nil
}

func deleteOrphanConnection(ctx context.Context, exec queryExecutor, connectionID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM connections WHERE id = $1`, connectionID); err != nil {
		return fmt.Errorf("delete orphan connection %d: %w", connectionID, err)
	}
	return nil
}
