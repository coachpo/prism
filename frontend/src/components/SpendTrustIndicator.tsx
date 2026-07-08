import { Link } from "@tanstack/react-router";
import { useLocale } from "@/i18n/useLocale";
import type { SpendTrustState } from "@/lib/costing";
import { cn } from "@/lib/utils";

interface SpendTrustNoteProps {
  spendTrust: SpendTrustState;
  className?: string;
  showPricingTemplatesLink?: boolean;
}

function getSpendTrustDescription(
  spendTrust: SpendTrustState,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  switch (spendTrust) {
    case "verified":
      return messages.spendTrust.verifiedDescription;
    case "fallback":
      return messages.spendTrust.fallbackDescription;
    case "unpriced":
      return messages.spendTrust.unpricedDescription;
  }
}

export function SpendTrustNote({
  spendTrust,
  className,
  showPricingTemplatesLink = false,
}: SpendTrustNoteProps) {
  const { messages } = useLocale();

  return (
    <div className={cn("flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground", className)}>
      <span>{getSpendTrustDescription(spendTrust, messages)}</span>
      {showPricingTemplatesLink ? (
        <Link
          className="font-medium text-primary underline-offset-4 hover:underline"
          to="/route/pricing"
        >
          {messages.spendTrust.openPricingTemplates}
        </Link>
      ) : null}
    </div>
  );
}
