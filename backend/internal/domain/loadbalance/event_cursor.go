package loadbalance

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// EventSortOrder values.
const (
	EventSortDesc = "desc"
	EventSortAsc  = "asc"
)

// EventQueryFilters are the server-before-pagination event filters. Repeatable
// enum filters OR within a field and AND across fields.
type EventQueryFilters struct {
	ModelID          *string
	EventTypes       []string
	FailureKinds     []string
	AdmissionReasons []string
	EndpointID       *int
	TerminalTargetID *int
}

// EventQueryBounds is a validated half-open event window; nil means unbounded.
type EventQueryBounds struct {
	FromTime *time.Time
	ToTime   *time.Time
}

func (bounds EventQueryBounds) normalized() EventQueryBounds {
	if bounds.FromTime != nil {
		from := bounds.FromTime.UTC()
		bounds.FromTime = &from
	}
	if bounds.ToTime != nil {
		to := bounds.ToTime.UTC()
		bounds.ToTime = &to
	}
	return bounds
}

// EventCursor is the opaque keyset cursor binding profile, event bounds,
// planning generation, canonical filters, sort order and limit.
type EventCursor struct {
	ProfileID          int
	BoundsFrom         *time.Time
	BoundsTo           *time.Time
	PlanningGeneration int64
	EventTypes         []string
	FailureKinds       []string
	AdmissionReasons   []string
	ModelID            *string
	EndpointID         *int
	TerminalTargetID   *int
	SortOrder          string
	Limit              int
	AfterCreatedAt     *time.Time
	AfterID            *int64
}

func (cursor EventCursor) sortKey() string { return cursor.SortOrder }

// canonicalFilters returns the canonical sorted filter list for binding.
func (cursor EventCursor) canonicalFilters() []string {
	items := make([]string, 0, 16)
	items = append(items, "m:"+nullableStringValue(cursor.ModelID), "e:"+nullableIntValue(cursor.EndpointID), "t:"+nullableIntValue(cursor.TerminalTargetID))
	items = append(items, cursor.EventTypes...)
	items = append(items, cursor.FailureKinds...)
	items = append(items, cursor.AdmissionReasons...)
	slices.Sort(items)
	return items
}

func nullableStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableIntValue(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func encodeEventCursor(cursor EventCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal event cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeEventCursor(raw string) (EventCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return EventCursor{}, fmt.Errorf("invalid event cursor")
	}
	var cursor EventCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return EventCursor{}, fmt.Errorf("invalid event cursor")
	}
	if cursor.ProfileID <= 0 || cursor.Limit < 1 || cursor.Limit > 100 || (cursor.SortOrder != EventSortDesc && cursor.SortOrder != EventSortAsc) {
		return EventCursor{}, fmt.Errorf("invalid event cursor")
	}
	return cursor, nil
}

func (cursor EventCursor) MatchesScope(profileID int, bounds EventQueryBounds, generation int64, filters EventQueryFilters, sortOrder string, limit int) bool {
	if cursor.ProfileID != profileID || cursor.SortOrder != sortOrder || cursor.Limit != limit || cursor.PlanningGeneration != generation {
		return false
	}
	expected := cursorWith(bounds, filters)
	return equalEventCursorScope(expected, cursor)
}

func cursorWith(bounds EventQueryBounds, filters EventQueryFilters) EventCursor {
	return EventCursor{
		BoundsFrom:       bounds.FromTime,
		BoundsTo:         bounds.ToTime,
		ModelID:          filters.ModelID,
		EndpointID:       filters.EndpointID,
		TerminalTargetID: filters.TerminalTargetID,
		EventTypes:       slices.Clone(filters.EventTypes),
		FailureKinds:     slices.Clone(filters.FailureKinds),
		AdmissionReasons: slices.Clone(filters.AdmissionReasons),
	}
}

func equalEventCursorScope(left EventCursor, right EventCursor) bool {
	if !nullableTimeEqual(left.BoundsFrom, right.BoundsFrom) || !nullableTimeEqual(left.BoundsTo, right.BoundsTo) {
		return false
	}
	if !nullableStringPtrEqual(left.ModelID, right.ModelID) || !nullableIntPtrEqual(left.EndpointID, right.EndpointID) || !nullableIntPtrEqual(left.TerminalTargetID, right.TerminalTargetID) {
		return false
	}
	return slices.Equal(left.EventTypes, right.EventTypes) &&
		slices.Equal(left.FailureKinds, right.FailureKinds) &&
		slices.Equal(left.AdmissionReasons, right.AdmissionReasons)
}

func nullableTimeEqual(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.UTC().Equal(right.UTC())
}

func nullableStringPtrEqual(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func nullableIntPtrEqual(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
