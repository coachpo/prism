package connections

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

func hydratePricingTemplateResponse(ctx context.Context, exec queryExecutor, item *pricingTemplateResponse) error {
	if item == nil || item.RevisionID <= 0 {
		return nil
	}
	rows, err := exec.Query(ctx, `
		SELECT card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price
		FROM pricing_template_cards WHERE revision_id = $1 ORDER BY card_role ASC`, item.RevisionID)
	if err != nil {
		return fmt.Errorf("load pricing template %d cards: %w", item.ID, err)
	}
	item.cards = make(map[string]pricingTemplateCard)
	for rows.Next() {
		var role, input, output string
		var cached, creation, reasoning sql.NullString
		if err := rows.Scan(&role, &input, &output, &cached, &creation, &reasoning); err != nil {
			return fmt.Errorf("scan pricing template %d card: %w", item.ID, err)
		}
		item.cards[role] = pricingTemplateCard{InputPrice: input, OutputPrice: output, CachedInputPrice: nullableStringValue(cached), CacheCreationPrice: nullableStringValue(creation), ReasoningPrice: nullableStringValue(reasoning)}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate pricing template %d cards: %w", item.ID, err)
	}
	rows.Close()
	windowRows, err := exec.Query(ctx, `
		SELECT weekday_mask, start_minute, end_minute
		FROM pricing_template_windows WHERE revision_id = $1
		ORDER BY weekday_mask, start_minute, end_minute`, item.RevisionID)
	if err != nil {
		return fmt.Errorf("load pricing template %d windows: %w", item.ID, err)
	}
	item.windows = make([]terminaltarget.Window, 0)
	for windowRows.Next() {
		var mask, start, end int
		if err := windowRows.Scan(&mask, &start, &end); err != nil {
			return fmt.Errorf("scan pricing template %d window: %w", item.ID, err)
		}
		item.windows = append(item.windows, terminaltarget.Window{WeekdayMask: mask, StartMinute: start, EndMinute: end})
	}
	if err := windowRows.Err(); err != nil {
		windowRows.Close()
		return fmt.Errorf("iterate pricing template %d windows: %w", item.ID, err)
	}
	windowRows.Close()
	item.projectCards()
	return nil
}

func (item *pricingTemplateResponse) projectCards() {
	if item == nil {
		return
	}
	threshold := 0
	if item.Tier != nil {
		threshold = item.Tier.InputTokensAbove
	}
	scheduleTimezone := ""
	if item.Schedule != nil {
		scheduleTimezone = item.Schedule.Timezone
	}
	item.Card = nil
	item.BaseCard = nil
	item.Tier = nil
	item.PeakCard = nil
	item.OffpeakCard = nil
	item.Schedule = nil
	for role, card := range item.cards {
		cardCopy := card
		switch role {
		case "standard":
			item.Card = &cardCopy
		case "tier_base":
			item.BaseCard = &cardCopy
		case "tier_above":
			if item.Tier == nil {
				item.Tier = &pricingTemplateTier{InputTokensAbove: threshold}
			}
			item.Tier.Card = &cardCopy
		case "peak":
			item.PeakCard = &cardCopy
		case "offpeak":
			item.OffpeakCard = &cardCopy
		}
	}
	if item.TemplateKind == "peak_valley" {
		if item.Schedule == nil {
			item.Schedule = &pricingTemplateSchedule{Timezone: scheduleTimezone}
		}
		item.Schedule.Windows = make([]pricingTemplateWindow, 0, len(item.windows))
		for _, window := range item.windows {
			item.Schedule.Windows = append(item.Schedule.Windows, pricingTemplateWindow{WeekdayMask: window.WeekdayMask, StartMinute: window.StartMinute, EndMinute: window.EndMinute})
		}
	}
}
