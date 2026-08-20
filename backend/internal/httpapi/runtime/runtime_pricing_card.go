package runtime

import (
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

type runtimePricingScheduleDecision int

const (
	runtimePricingScheduleUnconfigured runtimePricingScheduleDecision = iota
	runtimePricingScheduleUnresolved
	runtimePricingScheduleInWindow
	runtimePricingScheduleOutOfWindow
)

type runtimePricingWallClock struct {
	Weekday int
	Minute  int
	Valid   bool
}

func compileRuntimePricingSchedule(timezone string, windows []terminaltarget.Window) runtimePricingScheduleSnapshot {
	tz := strings.TrimSpace(timezone)
	if len(windows) == 0 {
		return runtimePricingScheduleSnapshot{Timezone: tz, State: runtimePricingScheduleUnconfigured}
	}
	compiled := terminaltarget.CompileRoutingSchedule(tz, windows)
	if compiled.Unresolved || compiled.Location == nil {
		return runtimePricingScheduleSnapshot{Timezone: tz, Location: compiled.Location, Windows: append([]terminaltarget.Window(nil), compiled.Windows...), State: runtimePricingScheduleUnresolved}
	}
	return runtimePricingScheduleSnapshot{Timezone: tz, Location: compiled.Location, Windows: append([]terminaltarget.Window(nil), compiled.Windows...), State: runtimePricingScheduleOutOfWindow}
}

func (s runtimePricingScheduleSnapshot) decideAt(referenceNow time.Time) (runtimePricingScheduleDecision, runtimePricingWallClock) {
	if s.State == runtimePricingScheduleUnconfigured || s.State == runtimePricingScheduleUnresolved || s.Location == nil {
		return s.State, runtimePricingWallClock{}
	}
	if referenceNow.IsZero() {
		return runtimePricingScheduleUnresolved, runtimePricingWallClock{}
	}
	local := referenceNow.In(s.Location)
	// terminaltarget.Window uses ISO weekdays (Monday=1..Sunday=7), while
	// time.Weekday uses Sunday=0. Keep the evidence in the same coordinate
	// system as the stored window rows.
	isoWeekday := ((int(local.Weekday()) + 6) % 7) + 1
	wall := runtimePricingWallClock{Weekday: isoWeekday, Minute: local.Hour()*60 + local.Minute(), Valid: true}
	compiled := terminaltarget.CompiledRoutingSchedule{Timezone: s.Timezone, Location: s.Location, Windows: s.Windows}
	switch compiled.DecideAt(referenceNow) {
	case terminaltarget.RoutingScheduleOpen:
		return runtimePricingScheduleInWindow, wall
	case terminaltarget.RoutingScheduleClosed:
		return runtimePricingScheduleOutOfWindow, wall
	default:
		return runtimePricingScheduleUnresolved, runtimePricingWallClock{}
	}
}

func runtimePricingWindowsDigest(windows []terminaltarget.Window) string {
	return terminaltarget.PricingWindowsDigest(windows)
}

type runtimePricingCardSelection struct {
	State               string
	Role                string
	Card                runtimePricingCard
	TierThresholdTokens *int
	TierBasisTokens     *int64
	DecidedAt           *time.Time
	Timezone            string
	LocalWeekday        *int
	LocalMinute         *int
	ScheduleDigest      string
	Incoherent          bool
}

func selectRuntimePricingCard(snapshot *runtimePricingTemplateSnapshot, usage responseUsage, operation string, referenceNow time.Time) runtimePricingCardSelection {
	if snapshot == nil {
		return runtimePricingCardSelection{State: pricingkind.SelectionUnresolved, Incoherent: true}
	}
	kind := pricingkind.Kind(strings.TrimSpace(snapshot.TemplateKind))
	if !kind.Valid() {
		return runtimePricingCardSelection{State: pricingkind.SelectionUnresolved, Incoherent: true}
	}
	selection := runtimePricingCardSelection{}
	switch kind {
	case pricingkind.Standard:
		selection.State = pricingkind.SelectionSelected
		selection.Role = pricingkind.RoleStandard
		selection.Card, selection.Incoherent = snapshotCardForRole(snapshot, selection.Role)
	case pricingkind.Tiered:
		if runtimePricingTierOperationIsTokenCount(operation) {
			selection.State = pricingkind.SelectionNotApplicable
			selection.Role = pricingkind.RoleTierBase
			selection.Card, selection.Incoherent = snapshotCardForRole(snapshot, selection.Role)
			return selection
		}
		if usage.InputTokens == nil || usage.OutputTokens == nil {
			selection.State = pricingkind.SelectionNotEvaluated
			return selection
		}
		if snapshot.TierInputTokensAbove == nil {
			selection.State = pricingkind.SelectionUnresolved
			selection.Incoherent = true
			return selection
		}
		basis, ok := runtimePricingTierBasisTokens(usage)
		if !ok {
			selection.State = pricingkind.SelectionUnresolved
			selection.Incoherent = true
			return selection
		}
		threshold := *snapshot.TierInputTokensAbove
		selection.TierThresholdTokens = intPtr(threshold)
		selection.TierBasisTokens = int64Ptr(basis)
		selection.Role = pricingkind.RoleTierBase
		if basis > int64(threshold) {
			selection.Role = pricingkind.RoleTierAbove
		}
		selection.State = pricingkind.SelectionSelected
		selection.Card, selection.Incoherent = snapshotCardForRole(snapshot, selection.Role)
	case pricingkind.PeakValley:
		selection.Timezone = snapshot.PricingSchedule.Timezone
		selection.ScheduleDigest = snapshot.PricingScheduleDigest
		if len(snapshot.PricingSchedule.Windows) == 0 ||
			strings.TrimSpace(snapshot.PricingScheduleDigest) == "" ||
			runtimePricingWindowsDigest(snapshot.PricingSchedule.Windows) != strings.TrimSpace(snapshot.PricingScheduleDigest) ||
			referenceNow.IsZero() {
			selection.State = pricingkind.SelectionUnresolved
			selection.Incoherent = true
			if !referenceNow.IsZero() {
				at := referenceNow.UTC()
				selection.DecidedAt = &at
			}
			return selection
		}
		decision, wall := snapshot.PricingSchedule.decideAt(referenceNow)
		if decision == runtimePricingScheduleUnconfigured || decision == runtimePricingScheduleUnresolved {
			selection.State = pricingkind.SelectionUnresolved
			selection.Incoherent = true
			if !referenceNow.IsZero() {
				at := referenceNow.UTC()
				selection.DecidedAt = &at
			}
			return selection
		}
		at := referenceNow.UTC()
		selection.DecidedAt = &at
		if wall.Valid {
			selection.LocalWeekday = intPtr(wall.Weekday)
			selection.LocalMinute = intPtr(wall.Minute)
		}
		selection.State = pricingkind.SelectionSelected
		if decision == runtimePricingScheduleInWindow {
			selection.Role = pricingkind.RolePeak
		} else {
			selection.Role = pricingkind.RoleOffpeak
		}
		selection.Card, selection.Incoherent = snapshotCardForRole(snapshot, selection.Role)
	default:
		selection.State = pricingkind.SelectionUnresolved
		selection.Incoherent = true
	}
	return selection
}

func snapshotCardForRole(snapshot *runtimePricingTemplateSnapshot, role string) (runtimePricingCard, bool) {
	if snapshot == nil {
		return runtimePricingCard{}, true
	}
	if card, ok := snapshot.card(role); ok {
		return card, false
	}
	// Transitional test fixtures and status-only callers still construct the
	// old scalar aliases. Keep them readable while production snapshots use
	// role-keyed cards loaded from pricing_template_cards.
	if role == pricingkind.RoleStandard || role == pricingkind.RoleTierBase {
		return runtimePricingCard{InputPrice: snapshot.InputPrice, OutputPrice: snapshot.OutputPrice, CachedInputPrice: snapshot.CachedInputPrice, CacheCreationPrice: snapshot.CacheCreationPrice, ReasoningPrice: snapshot.ReasoningPrice}, false
	}
	if role == pricingkind.RoleTierAbove {
		return runtimePricingCard{InputPrice: snapshot.TierInputPrice, OutputPrice: snapshot.TierOutputPrice, CachedInputPrice: snapshot.TierCachedInputPrice, CacheCreationPrice: snapshot.TierCacheCreationPrice, ReasoningPrice: snapshot.TierReasoningPrice}, false
	}
	return runtimePricingCard{}, true
}

func snapshotWithPricingCard(snapshot *runtimePricingTemplateSnapshot, card runtimePricingCard) *runtimePricingTemplateSnapshot {
	if snapshot == nil {
		return nil
	}
	copy := *snapshot
	copy.InputPrice = card.InputPrice
	copy.OutputPrice = card.OutputPrice
	copy.CachedInputPrice = card.CachedInputPrice
	copy.CacheCreationPrice = card.CacheCreationPrice
	copy.ReasoningPrice = card.ReasoningPrice
	return &copy
}

func cloneRuntimeTimePointer(source *time.Time) *time.Time {
	if source == nil {
		return nil
	}
	value := source.UTC()
	return &value
}

func applyRuntimePricingCardSelection(result *runtimePricingResult, selection runtimePricingCardSelection) {
	result.PricingSelectionState = stringPtr(selection.State)
	if selection.Role != "" {
		result.PricingCardRole = stringPtr(selection.Role)
	}
	result.PricingSelectorThresholdTokens = cloneRuntimeIntPointer(selection.TierThresholdTokens)
	result.PricingSelectorBasisTokens = cloneRuntimeInt64Pointer(selection.TierBasisTokens)
	result.PricingScheduleDecidedAt = cloneRuntimeTimePointer(selection.DecidedAt)
	if selection.Timezone != "" {
		result.PricingScheduleTimezone = stringPtr(selection.Timezone)
	}
	result.PricingScheduleLocalWeekday = cloneRuntimeIntPointer(selection.LocalWeekday)
	result.PricingScheduleLocalMinute = cloneRuntimeIntPointer(selection.LocalMinute)
	if selection.ScheduleDigest != "" {
		result.PricingScheduleDigest = stringPtr(selection.ScheduleDigest)
	}
}
