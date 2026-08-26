package connections

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

type pricingTemplateShape struct {
	Kind          pricingkind.Kind
	Cards         map[string]pricingTemplateCard
	TierThreshold *int
	Timezone      *string
	Windows       []terminaltarget.Window
	Digest        string
}

func pricingTemplateFieldError(path, reason, message string) error {
	return &domainError{
		StatusCode: http.StatusUnprocessableEntity,
		Detail:     "Invalid pricing template",
		Fields: map[string]any{
			"field": path, "path": path, "reason": reason, "message": message,
		},
	}
}

func normalizePricingTemplateShape(input pricingTemplateCreateRequest) (pricingTemplateShape, error) {
	kind := pricingkind.Kind(strings.TrimSpace(input.TemplateKind))
	if !kind.Valid() {
		return pricingTemplateShape{}, pricingTemplateFieldError("template_kind", "invalid_enum", "must be standard, tiered, or peak_valley")
	}
	if input.InputPrice != nil || input.OutputPrice != nil || input.CachedInputPrice != nil || input.CacheCreationPrice != nil || input.ReasoningPrice != nil || input.PricingUnit != nil || input.PricingCurrencyCode != nil || (input.Tier != nil && (input.Tier.Card == nil || input.Tier.InputPrice != nil || input.Tier.OutputPrice != nil || input.Tier.CachedInputPrice != nil || input.Tier.CacheCreationPrice != nil || input.Tier.ReasoningPrice != nil)) {
		return pricingTemplateShape{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "legacy pricing fields are not accepted; use the typed template shape"}
	}
	shape := pricingTemplateShape{Kind: kind, Cards: map[string]pricingTemplateCard{}}
	rejectVariantFields := func(condition bool, detail string) error {
		if condition {
			return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: detail}
		}
		return nil
	}
	switch kind {
	case pricingkind.Standard:
		if err := rejectVariantFields(input.BaseCard != nil || input.Tier != nil || input.PeakCard != nil || input.OffpeakCard != nil || input.Schedule != nil, "standard templates accept only card"); err != nil {
			return pricingTemplateShape{}, err
		}
	case pricingkind.Tiered:
		if err := rejectVariantFields(input.Card != nil || input.PeakCard != nil || input.OffpeakCard != nil || input.Schedule != nil, "tiered templates accept only base_card and tier"); err != nil {
			return pricingTemplateShape{}, err
		}
	case pricingkind.PeakValley:
		if err := rejectVariantFields(input.Card != nil || input.BaseCard != nil || input.Tier != nil, "peak_valley templates accept only peak_card, offpeak_card, and schedule"); err != nil {
			return pricingTemplateShape{}, err
		}
	}
	addCard := func(role, path string, cardInput *pricingTemplateCardInput) error {
		if cardInput == nil {
			return pricingTemplateFieldError(path, "required", "card is required")
		}
		card, err := normalizePricingTemplateCard(path, cardInput)
		if err != nil {
			return err
		}
		shape.Cards[role] = card
		return nil
	}
	switch kind {
	case pricingkind.Standard:
		if err := addCard(pricingkind.RoleStandard, "card", input.Card); err != nil {
			return pricingTemplateShape{}, err
		}
	case pricingkind.Tiered:
		if input.BaseCard == nil || input.Tier == nil || input.Tier.Card == nil {
			return pricingTemplateShape{}, pricingTemplateFieldError("base_card", "required", "tiered templates require base_card and tier.card")
		}
		if input.Tier.InputTokensAbove == nil || *input.Tier.InputTokensAbove < 1 {
			return pricingTemplateShape{}, pricingTemplateFieldError("tier.input_tokens_above", "invalid_range", "must be a positive integer")
		}
		shape.TierThreshold = intPtr(*input.Tier.InputTokensAbove)
		if err := addCard(pricingkind.RoleTierBase, "base_card", input.BaseCard); err != nil {
			return pricingTemplateShape{}, err
		}
		if err := addCard(pricingkind.RoleTierAbove, "tier.card", input.Tier.Card); err != nil {
			return pricingTemplateShape{}, err
		}
	case pricingkind.PeakValley:
		if err := addCard(pricingkind.RolePeak, "peak_card", input.PeakCard); err != nil {
			return pricingTemplateShape{}, err
		}
		if err := addCard(pricingkind.RoleOffpeak, "offpeak_card", input.OffpeakCard); err != nil {
			return pricingTemplateShape{}, err
		}
		if input.Schedule == nil {
			return pricingTemplateShape{}, pricingTemplateFieldError("schedule", "required", "peak_valley templates require a schedule")
		}
		windows, err := normalizePricingTemplateWindows(input.Schedule)
		if err != nil {
			return pricingTemplateShape{}, err
		}
		shape.Windows = windows
		tz := strings.TrimSpace(input.Schedule.Timezone)
		shape.Timezone = &tz
		shape.Digest = pricingTemplateWindowsDigest(windows)
	}
	if err := validatePricingTemplateCardParity(shape.Cards); err != nil {
		return pricingTemplateShape{}, err
	}
	return shape, nil
}

func normalizePricingTemplateCard(path string, input *pricingTemplateCardInput) (pricingTemplateCard, error) {
	for _, field := range []string{"input_price", "output_price", "cached_input_price", "cache_creation_price", "reasoning_price"} {
		if input.present != nil && !input.present[field] {
			return pricingTemplateCard{}, pricingTemplateFieldError(path+"."+field, "required", "field must be present; use null only for an unconfigured specialty price")
		}
	}
	inputPrice, err := normalizeRequiredPricingDecimalString(path+".input_price", input.InputPrice)
	if err != nil {
		return pricingTemplateCard{}, err
	}
	outputPrice, err := normalizeRequiredPricingDecimalString(path+".output_price", input.OutputPrice)
	if err != nil {
		return pricingTemplateCard{}, err
	}
	cached, err := normalizeOptionalPricingDecimalString(path+".cached_input_price", input.CachedInputPrice)
	if err != nil {
		return pricingTemplateCard{}, err
	}
	creation, err := normalizeOptionalPricingDecimalString(path+".cache_creation_price", input.CacheCreationPrice)
	if err != nil {
		return pricingTemplateCard{}, err
	}
	reasoning, err := normalizeOptionalPricingDecimalString(path+".reasoning_price", input.ReasoningPrice)
	if err != nil {
		return pricingTemplateCard{}, err
	}
	return pricingTemplateCard{InputPrice: inputPrice, OutputPrice: outputPrice, CachedInputPrice: cached, CacheCreationPrice: creation, ReasoningPrice: reasoning}, nil
}

func intPtr(value int) *int {
	return &value
}

func validatePricingTemplateCardParity(cards map[string]pricingTemplateCard) error {
	var reference *pricingTemplateCard
	for _, card := range cards {
		if reference == nil {
			copy := card
			reference = &copy
			continue
		}
		if (reference.CachedInputPrice == nil) != (card.CachedInputPrice == nil) ||
			(reference.CacheCreationPrice == nil) != (card.CacheCreationPrice == nil) ||
			(reference.ReasoningPrice == nil) != (card.ReasoningPrice == nil) {
			return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "all cards must mirror specialty price configuration"}
		}
	}
	return nil
}

func normalizePricingTemplateWindows(input *pricingTemplateScheduleInput) ([]terminaltarget.Window, error) {
	if len(input.Windows) == 0 {
		return nil, pricingTemplateFieldError("schedule.windows", "required", "at least one peak window is required")
	}
	if len(input.Windows) > terminaltarget.RoutingScheduleMaxWindows {
		return nil, pricingTemplateFieldError("schedule.windows", "limit_exceeded", "at most 32 windows are allowed")
	}
	tz := strings.TrimSpace(input.Timezone)
	if tz == "" || tz == "Local" {
		return nil, pricingTemplateFieldError("schedule.timezone", "invalid_timezone", "must be a valid IANA timezone")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return nil, pricingTemplateFieldError("schedule.timezone", "invalid_timezone", "IANA timezone is unknown")
	}
	seen := make(map[terminaltarget.Window]struct{}, len(input.Windows))
	windows := make([]terminaltarget.Window, 0, len(input.Windows))
	for index, raw := range input.Windows {
		window := terminaltarget.Window{WeekdayMask: raw.WeekdayMask, StartMinute: raw.StartMinute, EndMinute: raw.EndMinute}
		if err := terminaltarget.ValidateRoutingWindowFields(window.WeekdayMask, window.StartMinute, window.EndMinute); err != nil {
			return nil, pricingTemplateFieldError(fmt.Sprintf("schedule.windows[%d]", index), err.Reason, "window is invalid")
		}
		if _, ok := seen[window]; ok {
			return nil, pricingTemplateFieldError(fmt.Sprintf("schedule.windows[%d]", index), "duplicate", "duplicate window")
		}
		seen[window] = struct{}{}
		windows = append(windows, window)
	}
	return normalizePricingWindows(windows), nil
}

func normalizePricingWindows(windows []terminaltarget.Window) []terminaltarget.Window {
	for i := range windows {
		for j := i + 1; j < len(windows); j++ {
			if windows[j].WeekdayMask < windows[i].WeekdayMask ||
				(windows[j].WeekdayMask == windows[i].WeekdayMask && windows[j].StartMinute < windows[i].StartMinute) ||
				(windows[j].WeekdayMask == windows[i].WeekdayMask && windows[j].StartMinute == windows[i].StartMinute && windows[j].EndMinute < windows[i].EndMinute) {
				windows[i], windows[j] = windows[j], windows[i]
			}
		}
	}
	return windows
}

func pricingTemplateWindowsDigest(windows []terminaltarget.Window) string {
	return terminaltarget.PricingWindowsDigest(windows)
}
