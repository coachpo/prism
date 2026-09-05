import type { Messages } from "@/i18n/messages";
import type { PricingCard, PricingTemplate } from "@/lib/types";

/**
 * 一个模板要呈现的价格卡，按角色顺序而不是大小顺序。阶梯与峰谷各有两张卡，
 * 只呈现其中一张会让「基础档价」「跨档价」混成一个没有口径的数字。
 */
export function templateRateCards(
  template: PricingTemplate,
): Array<{ role: string; card: PricingCard | null }> {
  const kind = (template as { template_kind?: string }).template_kind;
  if (kind === "standard") return [{ role: "standard", card: template.card ?? null }];
  if (kind === "tiered")
    return [
      { role: "tier_base", card: template.base_card ?? null },
      { role: "tier_above", card: template.tier?.card ?? null },
    ];
  if (kind === "peak_valley")
    return [
      { role: "peak", card: template.peak_card ?? null },
      { role: "offpeak", card: template.offpeak_card ?? null },
    ];
  return [];
}

export function cardRoleLabel(role: string, copy: Messages["pricingTemplatesUi"]) {
  switch (role) {
    case "standard": return copy.cardStandard;
    case "tier_base": return copy.cardTierBase;
    case "tier_above": return copy.cardTierAbove;
    case "peak": return copy.cardPeak;
    case "offpeak": return copy.cardOffpeak;
    default: return role;
  }
}
