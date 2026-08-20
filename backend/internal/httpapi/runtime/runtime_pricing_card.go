package runtime

import (
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

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
	ScheduleUnresolved  bool
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
		card, ok := snapshot.card(pricingkind.RoleStandard)
		if !ok {
			return runtimePricingCardSelection{State: pricingkind.SelectionUnresolved, Incoherent: true}
		}
		selection.State = pricingkind.SelectionSelected
		selection.Role = pricingkind.RoleStandard
		selection.Card = card
	case pricingkind.Tiered:
		if runtimePricingTierOperationIsTokenCount(operation) {
			card, ok := snapshot.card(pricingkind.RoleTierBase)
			if !ok {
				return runtimePricingCardSelection{State: pricingkind.SelectionUnresolved, Incoherent: true}
			}
			selection.State = pricingkind.SelectionNotApplicable
			selection.Role = pricingkind.RoleTierBase
			selection.Card = card
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
		role := pricingkind.RoleTierBase
		if basis > int64(threshold) {
			role = pricingkind.RoleTierAbove
		}
		card, ok := snapshot.card(role)
		if !ok {
			return runtimePricingCardSelection{State: pricingkind.SelectionUnresolved, Incoherent: true}
		}
		selection.State = pricingkind.SelectionSelected
		selection.Role = role
		selection.Card = card
	case pricingkind.PeakValley:
		selection.Timezone = snapshot.PricingSchedule.Timezone
		selection.ScheduleDigest = snapshot.PricingScheduleDigest
		if len(snapshot.PricingSchedule.Windows) == 0 ||
			strings.TrimSpace(snapshot.PricingScheduleDigest) == "" ||
			runtimePricingWindowsDigest(snapshot.PricingSchedule.Windows) != strings.TrimSpace(snapshot.PricingScheduleDigest) ||
			referenceNow.IsZero() {
			selection.State = pricingkind.SelectionUnresolved
			selection.Incoherent = true
			selection.ScheduleUnresolved = true
			if !referenceNow.IsZero() {
				at := referenceNow.UTC()
				selection.DecidedAt = &at
			}
			return selection
		}
		decision, wall := snapshot.PricingSchedule.DecideAt(referenceNow)
		if decision == terminaltarget.PricingScheduleUnconfigured || decision == terminaltarget.PricingScheduleUnresolved {
			selection.State = pricingkind.SelectionUnresolved
			selection.Incoherent = true
			selection.ScheduleUnresolved = true
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
		role := pricingkind.RoleOffpeak
		if decision == terminaltarget.PricingScheduleInWindow {
			role = pricingkind.RolePeak
		} else {
			role = pricingkind.RoleOffpeak
		}
		card, ok := snapshot.card(role)
		if !ok {
			selection.State = pricingkind.SelectionUnresolved
			selection.Incoherent = true
			selection.LocalWeekday = nil
			selection.LocalMinute = nil
			return selection
		}
		selection.State = pricingkind.SelectionSelected
		selection.Role = role
		selection.Card = card
	default:
		selection.State = pricingkind.SelectionUnresolved
		selection.Incoherent = true
	}
	return selection
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
