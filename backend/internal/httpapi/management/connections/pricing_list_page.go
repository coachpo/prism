package connections

// Bounded Pricing list pages are the owner handoff used by Settings currency
// drafts and other large-template consumers. The legacy no-query list remains
// only for existing internal callers; every paged request is authenticated,
// keyset-ordered, generation-bound, and never loads the full active set.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

type pricingTemplateListPage struct {
	Items            []pricingTemplateListItem `json:"items"`
	TotalCount       int                       `json:"total_count"`
	ConsumedCount    int                       `json:"consumed_count"`
	ListSnapshotHash string                    `json:"list_snapshot_hash"`
	NextCursor       *string                   `json:"next_cursor"`
}

type pricingTemplateListItem struct {
	ID                           string             `json:"id"`
	ProfileID                    string             `json:"profile_id"`
	Name                         string             `json:"name"`
	Description                  *string            `json:"description"`
	CurrentRevision              pricingRevisionDTO `json:"current_revision"`
	ConfigurationStatus          string             `json:"configuration_status"`
	MissingSpecialtyComponents   []string           `json:"missing_specialty_components"`
	ModelReferenceCount          int                `json:"model_reference_count"`
	EndpointReferenceCount       int                `json:"endpoint_reference_count"`
	TerminalTargetReferenceCount int                `json:"terminal_target_reference_count"`
	CreatedAt                    string             `json:"created_at"`
	UpdatedAt                    string             `json:"updated_at"`
	DeletedAt                    *string            `json:"deleted_at"`
}

type pricingRevisionDTO struct {
	RevisionID           string               `json:"revision_id"`
	Version              int                  `json:"version"`
	PricingUnit          string               `json:"pricing_unit"`
	CurrencyCode         string               `json:"currency_code"`
	ReportingEpoch       *int                 `json:"reporting_currency_epoch"`
	CurrencyAttribution  string               `json:"currency_attribution"`
	Tier                 *pricingTemplateTier `json:"tier,omitempty"`
	TemplateKind         string               `json:"template_kind"`
	Card                 *pricingTemplateCard `json:"card,omitempty"`
	BaseCard             *pricingTemplateCard `json:"base_card,omitempty"`
	PeakCard             *pricingTemplateCard `json:"peak_card,omitempty"`
	OffpeakCard          *pricingTemplateCard `json:"offpeak_card,omitempty"`
	ScheduleTimezone     *string              `json:"schedule_timezone,omitempty"`
	ScheduleDigest       *string              `json:"schedule_digest,omitempty"`
	EffectiveAt          *string              `json:"effective_at"`
	CreatedAt            string               `json:"created_at"`
	CreatedByKind        string               `json:"created_by_kind"`
	CreatedByOperationID *string              `json:"created_by_operation_id"`
}

type pricingListCursor struct {
	Version          int    `json:"v"`
	ProfileID        int    `json:"profile"`
	Query            string `json:"q"`
	Generation       string `json:"generation"`
	SnapshotHash     string `json:"snapshot"`
	ConsumedCount    int    `json:"consumed"`
	LastNameIdentity []byte `json:"last_name_identity"`
	LastID           int    `json:"last_id"`
}

// startKey is the keyset lower bound the page walks from. The first page has
// no cursor, and pgx encodes a nil []byte as SQL NULL: every comparison
// against NULL is NULL, so a nil bound would drop the whole page while
// total_count still reported the real size. The empty prefix stays an empty
// bytea, which sorts before every real name_identity.
func (c pricingListCursor) startKey() []byte {
	if c.LastNameIdentity == nil {
		return []byte{}
	}
	return c.LastNameIdentity
}

func (s *Service) handleListPricingTemplatePage(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "pricing template page", func(tx pgx.Tx) (pricingTemplateListPage, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateListPage{}, err
		}
		limit, err := pricingTemplatePageLimit(r)
		if err != nil {
			return pricingTemplateListPage{}, err
		}
		queryText, err := pricingTemplatePageQuery(r)
		if err != nil {
			return pricingTemplateListPage{}, err
		}
		generation, err := loadPricingListGeneration(r.Context(), tx, profile.ID)
		if err != nil {
			return pricingTemplateListPage{}, err
		}
		snapshotHash := pricingListSnapshotHash(profile.ID, queryText, generation)
		cursor, err := s.decodePricingListCursor(r.URL.Query().Get("cursor"), pricingListCursor{ProfileID: profile.ID, Query: queryText, Generation: generation, SnapshotHash: snapshotHash})
		if err != nil {
			return pricingTemplateListPage{}, err
		}
		var total int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM pricing_templates WHERE profile_id = $1 AND deleted_at IS NULL AND ($2 = '' OR name LIKE '%' || $2 || '%')`, profile.ID, queryText).Scan(&total); err != nil {
			return pricingTemplateListPage{}, fmt.Errorf("count pricing template page: %w", err)
		}
		rows, err := tx.Query(r.Context(), `
			SELECT templates.id, templates.profile_id, templates.name, templates.description,
				templates.created_at, templates.updated_at, templates.deleted_at,
				revisions.id, revisions.version, revisions.pricing_unit, revisions.currency_code,
				revisions.currency_attribution, revisions.reporting_currency_epoch, revisions.template_kind,
				revisions.tier_input_tokens_above, revisions.pricing_schedule_timezone, revisions.pricing_schedule_digest,
				revisions.effective_at, revisions.created_at, revisions.created_by_kind, revisions.created_by_operation_id,
				templates.name_identity,
				(SELECT count(DISTINCT targets.source_model_config_id) FROM model_access_targets AS targets JOIN connections AS refs ON refs.id = targets.target_connection_id WHERE refs.profile_id = $1 AND refs.pricing_template_id = templates.id AND targets.profile_id = $1 AND targets.target_type = 'connection'),
				(SELECT count(DISTINCT refs.endpoint_id) FROM connections AS refs WHERE refs.profile_id = $1 AND refs.pricing_template_id = templates.id),
				(SELECT count(DISTINCT refs.id) FROM connections AS refs WHERE refs.profile_id = $1 AND refs.pricing_template_id = templates.id)
			FROM pricing_templates AS templates
			LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id
			WHERE templates.profile_id = $1 AND templates.deleted_at IS NULL AND ($2 = '' OR templates.name LIKE '%' || $2 || '%')
				AND (templates.name_identity > $3 OR (templates.name_identity = $3 AND templates.id > $4))
			ORDER BY templates.name_identity ASC, templates.id ASC LIMIT $5`, profile.ID, queryText, cursor.startKey(), cursor.LastID, limit)
		if err != nil {
			return pricingTemplateListPage{}, fmt.Errorf("load pricing template page: %w", err)
		}
		defer rows.Close()
		items := make([]pricingTemplateListItem, 0, limit)
		lastName := cursor.startKey()
		lastID := cursor.LastID
		for rows.Next() {
			item, nameIdentity, err := scanPricingTemplateListItem(rows)
			if err != nil {
				return pricingTemplateListPage{}, err
			}
			items = append(items, item)
			lastName, lastID = nameIdentity, int(parseDecimalID(item.ID))
		}
		if err := rows.Err(); err != nil {
			return pricingTemplateListPage{}, err
		}
		for index := range items {
			if err := hydratePricingListRevision(r.Context(), tx, &items[index].CurrentRevision); err != nil {
				return pricingTemplateListPage{}, err
			}
		}
		consumed := cursor.ConsumedCount + len(items)
		page := pricingTemplateListPage{Items: items, TotalCount: total, ConsumedCount: consumed, ListSnapshotHash: snapshotHash}
		if len(items) > 0 {
			var remaining int
			if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM pricing_templates WHERE profile_id = $1 AND deleted_at IS NULL AND ($2 = '' OR name LIKE '%' || $2 || '%') AND (name_identity > $3 OR (name_identity = $3 AND id > $4))`, profile.ID, queryText, lastName, lastID).Scan(&remaining); err != nil {
				return pricingTemplateListPage{}, err
			}
			if remaining > 0 {
				value := s.encodePricingListCursor(pricingListCursor{ProfileID: profile.ID, Query: queryText, Generation: generation, SnapshotHash: snapshotHash, ConsumedCount: consumed, LastNameIdentity: lastName, LastID: lastID})
				page.NextCursor = &value
			}
		}
		return page, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func pricingTemplatePageLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 50, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 100 {
		return 0, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "limit must be between 1 and 100"}
	}
	return value, nil
}

func pricingTemplatePageQuery(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(value) > 100 {
		return "", &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "q must be at most 100 characters"}
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "q contains unsupported control characters"}
		}
	}
	return value, nil
}

func loadPricingListGeneration(ctx context.Context, tx pgx.Tx, profileID int) (string, error) {
	var templateGeneration, referenceGeneration int64
	if err := tx.QueryRow(ctx, `SELECT pricing_template_generation, pricing_reference_generation FROM user_settings WHERE profile_id = $1`, profileID).Scan(&templateGeneration, &referenceGeneration); err != nil {
		return "", fmt.Errorf("load pricing list generation: %w", err)
	}
	return strconv.FormatInt(templateGeneration, 10) + ":" + strconv.FormatInt(referenceGeneration, 10), nil
}

func pricingListSnapshotHash(profileID int, query, generation string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", profileID, query, generation)))
	return fmt.Sprintf("%x", sum[:])
}

func (s *Service) pricingCursorKey() []byte {
	seed := s.secretEncryptionKey
	if seed == "" {
		seed = "pricing-template-page-cursor"
	}
	sum := sha256.Sum256([]byte("prism.pricing.list.cursor.v1:" + seed))
	return sum[:]
}

func (s *Service) encodePricingListCursor(cursor pricingListCursor) string {
	cursor.Version = 1
	payload, _ := json.Marshal(cursor)
	mac := hmac.New(sha256.New, s.pricingCursorKey())
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
}

func (s *Service) decodePricingListCursor(raw string, expected pricingListCursor) (pricingListCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return pricingListCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) <= sha256.Size {
		return pricingListCursor{}, &DomainError{StatusCode: http.StatusBadRequest, Detail: "pricing_list_cursor_invalid"}
	}
	payload, signature := decoded[:len(decoded)-sha256.Size], decoded[len(decoded)-sha256.Size:]
	mac := hmac.New(sha256.New, s.pricingCursorKey())
	_, _ = mac.Write(payload)
	var cursor pricingListCursor
	if !hmac.Equal(signature, mac.Sum(nil)) || json.Unmarshal(payload, &cursor) != nil || cursor.Version != 1 || cursor.ProfileID != expected.ProfileID || cursor.Query != expected.Query || cursor.Generation != expected.Generation || cursor.SnapshotHash != expected.SnapshotHash || cursor.ConsumedCount < 0 || cursor.LastID < 0 {
		return pricingListCursor{}, &DomainError{StatusCode: http.StatusBadRequest, Detail: "pricing_list_cursor_invalid"}
	}
	return cursor, nil
}

func scanPricingTemplateListItem(scanner interface{ Scan(...any) error }) (pricingTemplateListItem, []byte, error) {
	var item pricingTemplateListItem
	var id, profileID int
	var name string
	var description, pricingUnit, currencyCode, currencyAttribution, templateKind, scheduleTimezone, scheduleDigest, createdByKind, createdByOperationID sql.NullString
	var deletedAt, effectiveAt, revisionCreatedAt sql.NullTime
	var revisionID sql.NullInt64
	var revisionEpoch, tierThreshold sql.NullInt32
	var version int
	var createdAt, updatedAt time.Time
	var nameIdentity []byte
	if err := scanner.Scan(&id, &profileID, &name, &description, &createdAt, &updatedAt, &deletedAt, &revisionID, &version, &pricingUnit, &currencyCode, &currencyAttribution, &revisionEpoch, &templateKind, &tierThreshold, &scheduleTimezone, &scheduleDigest, &effectiveAt, &revisionCreatedAt, &createdByKind, &createdByOperationID, &nameIdentity, &item.ModelReferenceCount, &item.EndpointReferenceCount, &item.TerminalTargetReferenceCount); err != nil {
		return pricingTemplateListItem{}, nil, err
	}
	if !revisionID.Valid || !pricingUnit.Valid || !currencyCode.Valid || !currencyAttribution.Valid || !templateKind.Valid {
		return pricingTemplateListItem{}, nil, &DomainError{StatusCode: http.StatusConflict, Detail: "pricing_template_shape_unavailable"}
	}
	item.ID = strconv.Itoa(id)
	item.ProfileID = strconv.Itoa(profileID)
	item.Name = name
	item.Description = nullableStringValue(description)
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	if deletedAt.Valid {
		value := deletedAt.Time.UTC().Format(time.RFC3339Nano)
		item.DeletedAt = &value
	}
	var effective, revisionCreated string
	if effectiveAt.Valid {
		effective = effectiveAt.Time.UTC().Format(time.RFC3339Nano)
	}
	if revisionCreatedAt.Valid {
		revisionCreated = revisionCreatedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	item.CurrentRevision = pricingRevisionDTO{RevisionID: strconv.FormatInt(revisionID.Int64, 10), Version: version, PricingUnit: pricingUnit.String, CurrencyCode: currencyCode.String, CurrencyAttribution: currencyAttribution.String, ReportingEpoch: nullableInt32Value(revisionEpoch), EffectiveAt: nullableTimeString(effective), CreatedAt: revisionCreated, CreatedByKind: createdByKind.String, CreatedByOperationID: nullableStringValue(createdByOperationID), TemplateKind: templateKind.String, ScheduleTimezone: nullableStringValue(scheduleTimezone), ScheduleDigest: nullableStringValue(scheduleDigest)}
	if tierThreshold.Valid {
		item.CurrentRevision.Tier = &pricingTemplateTier{InputTokensAbove: int(tierThreshold.Int32)}
	}
	item.ConfigurationStatus = "complete"
	item.MissingSpecialtyComponents = []string{}
	return item, nameIdentity, nil
}

func hydratePricingListRevision(ctx context.Context, tx pgx.Tx, revision *pricingRevisionDTO) error {
	if revision == nil || revision.RevisionID == "" {
		return nil
	}
	revisionID, err := strconv.ParseInt(revision.RevisionID, 10, 64)
	if err != nil || revisionID < 1 {
		return fmt.Errorf("invalid pricing revision id %q", revision.RevisionID)
	}
	rows, err := tx.Query(ctx, `SELECT card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price FROM pricing_template_cards WHERE revision_id = $1 ORDER BY card_role`, revisionID)
	if err != nil {
		return fmt.Errorf("load pricing list revision cards: %w", err)
	}
	defer rows.Close()
	if revision.Tier != nil {
		revision.Tier.Card = nil
	}
	for rows.Next() {
		var role, input, output string
		var cached, creation, reasoning sql.NullString
		if err := rows.Scan(&role, &input, &output, &cached, &creation, &reasoning); err != nil {
			return err
		}
		card := &pricingTemplateCard{InputPrice: input, OutputPrice: output, CachedInputPrice: nullableStringValue(cached), CacheCreationPrice: nullableStringValue(creation), ReasoningPrice: nullableStringValue(reasoning)}
		switch role {
		case "standard":
			revision.Card = card
		case "tier_base":
			revision.BaseCard = card
		case "tier_above":
			if revision.Tier == nil {
				revision.Tier = &pricingTemplateTier{}
			}
			revision.Tier.Card = card
		case "peak":
			revision.PeakCard = card
		case "offpeak":
			revision.OffpeakCard = card
		}
	}
	return rows.Err()
}

func parseDecimalID(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func nullableInt32Value(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int32)
	return &result
}

func nullableTimeString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
