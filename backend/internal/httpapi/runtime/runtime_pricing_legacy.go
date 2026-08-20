package runtime

import "github.com/coachpo/prism/backend/internal/domain/pricingkind"

func copyLegacyPricingCardAliases(snapshot *runtimePricingTemplateSnapshot) {
	if snapshot == nil {
		return
	}
	copyCard := func(role string) (runtimePricingCard, bool) {
		return snapshot.card(role)
	}
	if card, ok := copyCard(pricingkind.RoleStandard); ok {
		snapshot.InputPrice, snapshot.OutputPrice = card.InputPrice, card.OutputPrice
		snapshot.CachedInputPrice, snapshot.CacheCreationPrice = card.CachedInputPrice, card.CacheCreationPrice
		snapshot.ReasoningPrice = card.ReasoningPrice
	}
	if card, ok := copyCard(pricingkind.RoleTierBase); ok {
		snapshot.InputPrice, snapshot.OutputPrice = card.InputPrice, card.OutputPrice
		snapshot.CachedInputPrice, snapshot.CacheCreationPrice = card.CachedInputPrice, card.CacheCreationPrice
		snapshot.ReasoningPrice = card.ReasoningPrice
	}
	if card, ok := copyCard(pricingkind.RoleTierAbove); ok {
		snapshot.TierInputPrice, snapshot.TierOutputPrice = card.InputPrice, card.OutputPrice
		snapshot.TierCachedInputPrice, snapshot.TierCacheCreationPrice = card.CachedInputPrice, card.CacheCreationPrice
		snapshot.TierReasoningPrice = card.ReasoningPrice
	}
}
