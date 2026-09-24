import { useLocale } from "@/i18n/useLocale";
import { OperatorTypeBadge } from "@/shared/design-system";

/**
 * 已计价是常态，不再单独占一列、每行重复一次；其余定价状态跟在金额旁边，
 * 缺价格的请求因此不会被读成零成本。后端枚举一律经中文标签再上屏。
 */
export function PricingStatusBadge({ status }: { status: string }) {
  const copy = useLocale().messages.observe;
  if (status === "priced") return null;
  return (
    <OperatorTypeBadge
      intent={
        status === "unpriced"
          ? "degraded"
          : status === "ineligible"
            ? "idle"
            : "failing"
      }
      label={
        status === "unpriced"
          ? copy.pricingUnpriced
          : status === "ineligible"
            ? copy.pricingIneligible
            : copy.pricingUnknown
      }
      preserveLabel
    />
  );
}
